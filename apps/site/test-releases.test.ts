import { test } from "node:test";
import assert from "node:assert/strict";
import { parseReleases, releasesURL } from "./lib/releases";
import {
  D1ReleaseStore,
  ReleaseService,
  type ReleaseRow,
  type ReleaseStore,
} from "./lib/release-cache";
import { DatabaseSync } from "node:sqlite";
import { readFileSync } from "node:fs";
import type { D1Database } from "@cloudflare/workers-types";

function fixture(version = "0.1.0-rc.1") {
  const tag = "v" + version;
  const names = [
    "macos-arm64.tar.gz",
    "macos-x64.tar.gz",
    "windows-x64.zip",
    "windows-arm64.zip",
    "linux-x64.tar.gz",
    "linux-arm64.tar.gz",
    "android-arm64.apk",
    "ios-unsigned.ipa",
  ];
  return {
    tag_name: tag,
    draft: false,
    prerelease: true,
    published_at: "2026-11-14T00:00:00Z",
    assets: [
      ...names.map((name) => `reign-${version}-${name}`),
      "SHA256SUMS",
    ].map((name) => ({
      name,
      size: 12345678,
      browser_download_url: `${releasesURL}/download/${tag}/${name}`,
    })),
  };
}
class MemoryStore implements ReleaseStore {
  row: ReleaseRow = { body: null, etag: null, checked_at: 0, refresh_after: 0 };
  token: string | null = null;
  until = 0;
  async read() {
    return { ...this.row };
  }
  async acquire(now: number, token: string) {
    if (this.row.refresh_after > now || this.until > now) return false;
    this.until = now + 20;
    this.token = token;
    return true;
  }
  async save(token: string, row: ReleaseRow) {
    if (token === this.token) {
      this.row = { ...row };
      this.until = 0;
      this.token = null;
    }
  }
}
test("published release assets match every CI client platform, excluding untrusted URLs and development signatures", () => {
  const release = fixture();
  release.assets.push({
    name: "reign-0.1.0-rc.1-android-arm64-debug.apk",
    size: 12,
    browser_download_url: `${releasesURL}/download/v0.1.0-rc.1/debug.apk`,
  });
  release.assets.push({
    name: "reign-0.1.0-rc.1-windows-x64.zip",
    size: 123,
    browser_download_url: "https://malicious.test/download.exe",
  });
  const parsed = parseReleases([release])!;
  assert.equal(parsed.version, "0.1.0-rc.1");
  assert.equal(parsed.assets.length, 8);
  assert.ok(parsed.checksumsURL?.endsWith("/SHA256SUMS"));
  assert.ok(
    parsed.assets.every((a) =>
      a.url.startsWith("https://github.com/monkeyWie/reign/releases/download/"),
    ),
  );
  const draft = { ...fixture("0.2.0"), draft: true };
  assert.equal(parseReleases([draft, release])?.version, "0.1.0-rc.1");
  assert.equal(parseReleases([]), null);
  assert.throws(() => parseReleases({ message: "rate limit" }));
});
test("one upstream lookup for a burst of requests, including across isolated Worker services", async () => {
  const store = new MemoryStore();
  let requests = 0;
  const fetcher: typeof fetch = async () => {
    requests++;
    await new Promise((r) => setTimeout(r, 10));
    return Response.json([fixture()], { headers: { etag: '"v1"' } });
  };
  const services = Array.from(
    { length: 20 },
    () => new ReleaseService(store, fetcher, () => 10000),
  );
  await Promise.all(
    Array.from({ length: 500 }, (_, i) => services[i % services.length].get()),
  );
  assert.equal(requests, 1);
  for (const service of services)
    assert.equal(
      (await new ReleaseService(store, fetcher, () => 10001).get()).release
        ?.version,
      "0.1.0-rc.1",
    );
  assert.equal(requests, 1);
});
test("ETag revalidation refreshes the shared snapshot without redownloading metadata", async () => {
  const store = new MemoryStore();
  let now = 10000,
    count = 0;
  const service = new ReleaseService(
    store,
    async (_url, options) => {
      count++;
      if (count === 1)
        return Response.json([fixture()], { headers: { etag: '"v1"' } });
      assert.equal(
        (options?.headers as Record<string, string>)["If-None-Match"],
        '"v1"',
      );
      return new Response(null, { status: 304 });
    },
    () => now,
  );
  await service.get();
  now += 901;
  const next = await service.get();
  assert.equal(count, 2);
  assert.equal(next.stale, false);
  assert.equal(next.release?.assets.length, 8);
});
test("429 respects GitHub's reset time and serves the last working downloads throughout the outage", async () => {
  const store = new MemoryStore();
  let now = 10000,
    requests = 0;
  const service = new ReleaseService(
    store,
    async () => {
      requests++;
      return requests === 1
        ? Response.json([fixture()])
        : Response.json(
            {},
            {
              status: 429,
              headers: { "x-ratelimit-reset": "15000", "retry-after": "120" },
            },
          );
    },
    () => now,
  );
  await service.get();
  now = 11000;
  assert.equal((await service.get()).stale, true);
  now = 14000;
  assert.equal((await service.get()).release?.version, "0.1.0-rc.1");
  assert.equal(requests, 2);
  assert.equal(store.row.refresh_after, 15000);
});
test("network and malformed-response failures keep a cached release, cold failures are negatively cached", async () => {
  for (const fail of [
    async () => {
      throw new Error("offline");
    },
    async () => Response.json({ unexpected: true }),
  ]) {
    const store = new MemoryStore();
    let count = 0;
    const fetcher: typeof fetch = async () => {
      count++;
      return fail();
    };
    const cold = new ReleaseService(store, fetcher, () => 10000);
    assert.equal((await cold.get()).release, null);
    assert.equal(
      (await new ReleaseService(store, fetcher, () => 10030).get()).checkedAt,
      null,
    );
    assert.equal(count, 1);
    store.row = {
      ...store.row,
      body: JSON.stringify(parseReleases([fixture()])),
      checked_at: 5000,
      refresh_after: 0,
    };
    assert.equal(
      (await new ReleaseService(store, fetcher, () => 11000).get()).release
        ?.assets.length,
      8,
    );
  }
});
test("actual SQLite schema enforces leases and rejects stale lease writers", async () => {
  const db = new DatabaseSync(":memory:");
  db.exec(
    readFileSync(
      new URL(
        "../../packages/worker-core/migrations/0002_release_cache.sql",
        import.meta.url,
      ),
      "utf8",
    ),
  );
  // Execute exactly the SQL used by the D1 adapter against real SQLite.
  const binding = {
    prepare(sql: string) {
      return {
        bind(...values: (string | number | null)[]) {
          return {
            async first() {
              return db.prepare(sql).get(...values) ?? null;
            },
            async run() {
              return db.prepare(sql).run(...values);
            },
          };
        },
      };
    },
  } as unknown as D1Database;
  const store = new D1ReleaseStore(binding);
  assert.equal((await store.read()).body, null);
  assert.equal(await store.acquire(10000, "first"), true);
  assert.equal(await store.acquire(10001, "other"), false);
  assert.equal(await store.acquire(10021, "newer"), true);
  await store.save("first", {
    body: "old",
    etag: null,
    checked_at: 10000,
    refresh_after: 20000,
  });
  assert.equal((await store.read()).body, null);
  await store.save("newer", {
    body: "new",
    etag: '"etag"',
    checked_at: 10021,
    refresh_after: 20000,
  });
  assert.equal((await store.read()).body, "new");
  assert.equal(await store.acquire(10022, "extra"), false);
  db.close();
});
