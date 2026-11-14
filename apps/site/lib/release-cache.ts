import type { D1Database, ExecutionContext } from "@cloudflare/workers-types";
import {
  parseReleases,
  releaseRepository,
  type ReleaseCatalog,
  type ReleaseVersion,
} from "./releases";

const key = "github-releases:" + releaseRepository;
const refreshSeconds = 15 * 60;
export interface ReleaseRow {
  body: string | null;
  etag: string | null;
  checked_at: number;
  refresh_after: number;
}
export interface ReleaseStore {
  read(): Promise<ReleaseRow>;
  acquire(now: number, token: string): Promise<boolean>;
  save(token: string, row: ReleaseRow): Promise<void>;
}
export class D1ReleaseStore implements ReleaseStore {
  constructor(private db: D1Database) {}
  async read(): Promise<ReleaseRow> {
    await this.db
      .prepare("INSERT OR IGNORE INTO release_cache(id) VALUES (?)")
      .bind(key)
      .run();
    return (await this.db
      .prepare(
        "SELECT body, etag, checked_at, refresh_after FROM release_cache WHERE id = ?",
      )
      .bind(key)
      .first<ReleaseRow>())!;
  }
  async acquire(now: number, token: string): Promise<boolean> {
    const row = await this.db
      .prepare(
        "UPDATE release_cache SET lease_until = ?, lease_token = ? WHERE id = ? AND refresh_after <= ? AND lease_until <= ? RETURNING id",
      )
      .bind(now + 20, token, key, now, now)
      .first();
    return !!row;
  }
  async save(token: string, row: ReleaseRow): Promise<void> {
    await this.db
      .prepare(
        "UPDATE release_cache SET body = ?, etag = ?, checked_at = ?, refresh_after = ?, lease_until = 0, lease_token = NULL WHERE id = ? AND lease_token = ?",
      )
      .bind(row.body, row.etag, row.checked_at, row.refresh_after, key, token)
      .run();
  }
}
function retryTime(response: Response | null, now: number): number {
  const retry = response?.headers.get("retry-after");
  const reset = Number(response?.headers.get("x-ratelimit-reset") || 0);
  const delay = retry
    ? /^\d+$/.test(retry)
      ? now + Number(retry)
      : Date.parse(retry) / 1000
    : 0;
  // Back off even for upstream 5xx/network failures; rate-limit headers win.
  return Math.ceil(
    Math.max(
      now + 5 * 60,
      Number.isFinite(delay) ? delay : 0,
      Number.isFinite(reset) ? reset : 0,
    ),
  );
}
export class ReleaseService {
  private inFlight: Promise<ReleaseCatalog> | null = null;
  private cached: { until: number; value: ReleaseCatalog } | null = null;
  constructor(
    private store: ReleaseStore,
    private fetcher: typeof fetch = fetch,
    private clock: () => number = () => Math.floor(Date.now() / 1000),
    private token?: string,
  ) {}
  get(): Promise<ReleaseCatalog> {
    const now = this.clock();
    if (this.cached && now < this.cached.until)
      return Promise.resolve(this.cached.value);
    if (this.inFlight) return this.inFlight;
    this.inFlight = this.load()
      .then((value) => {
        this.cached = {
          until: this.clock() + (value.checkedAt ? 60 : 5),
          value,
        };
        return value;
      })
      .finally(() => {
        this.inFlight = null;
      });
    return this.inFlight;
  }
  private async load(): Promise<ReleaseCatalog> {
    const now = this.clock();
    let row = await this.store.read();
    if (now >= row.refresh_after) {
      const lease = crypto.randomUUID();
      if (await this.store.acquire(now, lease)) {
        let response: Response | null = null;
        try {
          const headers: Record<string, string> = {
            Accept: "application/vnd.github+json",
            "User-Agent": "Reign-Release-Catalog",
            "X-GitHub-Api-Version": "2022-11-28",
          };
          if (row.etag) headers["If-None-Match"] = row.etag;
          if (this.token) headers.Authorization = `Bearer ${this.token}`;
          response = await this.fetcher(
            `https://api.github.com/repos/${releaseRepository}/releases?per_page=10`,
            { headers, signal: AbortSignal.timeout(8000) },
          );
          if (response.status === 304 && row.body !== null) {
            row = {
              ...row,
              checked_at: now,
              refresh_after: now + (row.body === "null" ? 60 : refreshSeconds),
            };
          } else {
            if (!response.ok) throw new Error("GitHub releases unavailable");
            const release = parseReleases(await response.json());
            row = {
              body: JSON.stringify(release),
              etag: response.headers.get("etag"),
              checked_at: now,
              refresh_after: now + (release ? refreshSeconds : 60),
            };
          }
        } catch {
          // Keep the last good metadata for outages, malformed responses and 429.
          row = { ...row, refresh_after: retryTime(response, now) };
        }
        await this.store.save(lease, row);
      } else {
        row = await this.store.read();
      }
    }
    return {
      release: row.body
        ? (JSON.parse(row.body) as ReleaseVersion | null)
        : null,
      checkedAt: row.checked_at
        ? new Date(row.checked_at * 1000).toISOString()
        : null,
      stale: !row.checked_at || now - row.checked_at >= refreshSeconds,
    };
  }
}

export interface ReleaseEnv {
  DB: D1Database;
  GITHUB_RELEASE_TOKEN?: string;
}
// DB bindings may have a new wrapper per request. One fixed catalogue per Worker
// isolate, plus the D1 lease, coalesces requests within and across data centres.
let service: ReleaseService | undefined;
export async function handleReleaseCatalog(
  request: Request,
  env: ReleaseEnv,
  ctx: ExecutionContext,
): Promise<Response | null> {
  if (new URL(request.url).pathname !== "/api/releases") return null;
  if (request.method !== "GET" && request.method !== "HEAD")
    return new Response(null, { status: 405, headers: { Allow: "GET, HEAD" } });
  const cacheKey = new Request(new URL("/api/releases", request.url));
  const edge =
    typeof caches !== "undefined"
      ? (caches as CacheStorage & { default?: Cache }).default
      : undefined;
  const hit = await edge?.match(cacheKey);
  if (hit) return request.method === "HEAD" ? new Response(null, hit) : hit;
  service ??= new ReleaseService(
    new D1ReleaseStore(env.DB),
    fetch,
    undefined,
    env.GITHUB_RELEASE_TOKEN,
  );
  try {
    const value = await service.get();
    const response = Response.json(value, {
      headers: {
        "Cache-Control": value.release
          ? "public, max-age=60, s-maxage=300, stale-while-revalidate=600, stale-if-error=86400"
          : value.checkedAt
            ? "public, max-age=30, s-maxage=30"
            : "public, max-age=5, s-maxage=5",
        "X-Content-Type-Options": "nosniff",
      },
    });
    if (edge)
      ctx.waitUntil(edge.put(cacheKey, response.clone()).catch(() => {}));
    return request.method === "HEAD" ? new Response(null, response) : response;
  } catch {
    return Response.json(
      { release: null, checkedAt: null, stale: true },
      {
        status: 503,
        headers: { "Cache-Control": "public, max-age=30", "Retry-After": "30" },
      },
    );
  }
}
