import { getCloudflareContext } from "@opennextjs/cloudflare";
import { connection } from "next/server";
import { getReleaseCatalog, type ReleaseEnv } from "./release-cache";
import type { ReleaseCatalog } from "./releases";

export async function getServerReleaseCatalog(): Promise<ReleaseCatalog> {
  // Resolve runtime bindings only for an incoming request, never at build time.
  await connection();
  const { env, ctx } = getCloudflareContext();
  try {
    return await getReleaseCatalog(env as unknown as ReleaseEnv, (task) =>
      ctx.waitUntil(task),
    );
  } catch {
    // Keep the download page and GitHub link usable during a cache outage.
    console.warn("release_catalog_unavailable");
    return { release: null, checkedAt: null, stale: true };
  }
}
