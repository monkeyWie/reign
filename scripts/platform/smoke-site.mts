// Read-only smoke checks, reusable against local builds and deployed sites.
import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

export async function smokeSite(origin: string) {
  const pending = new Set([
    "/",
    "/features",
    "/technology",
    "/pricing",
    "/download",
    "/docs",
  ]);
  const visited = new Set<string>();
  while (pending.size) {
    const path = pending.values().next().value!;
    pending.delete(path);
    const response = await fetch(origin + path);
    assert.equal(response.status, 200, path);
    const html = await response.text();
    assert.match(html, /Reign/, path);
    assert.match(html, /<h1[\s>]/, path);
    visited.add(path);
    for (const match of html.matchAll(
      /href="(\/docs(?:\/[^"?#]*)?)(?:[?#][^"]*)?"/g,
    )) {
      const linked = match[1];
      if (!visited.has(linked)) pending.add(linked);
    }
  }
  assert.ok(
    visited.has("/docs/self-hosting/cloudflare"),
    "Self-hosting docs must be linked",
  );
  assert.ok(
    visited.has("/docs/contributing/development"),
    "Contribution docs must be linked",
  );
  assert.ok(
    visited.has("/docs/architecture/stack"),
    "Technology docs must be linked",
  );
  assert.equal((await fetch(origin + "/docs/not-a-reign-page")).status, 404);
  const search = await fetch(origin + "/api/search");
  assert.equal(search.status, 200);
  const index = await search.json();
  assert.equal(index.type, "advanced");
  const serialized = JSON.stringify(index);
  assert.match(serialized, /自建/);
  assert.match(serialized, /Codex/);
  for (const path of visited) {
    if (path.startsWith("/docs"))
      assert.ok(serialized.includes(path), `Search index missing ${path}`);
  }
  console.log(
    `Website, ${visited.size - 5} linked documentation pages, unknown-page 404 and browser search export passed`,
  );
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  await smokeSite(process.argv[2] || "http://127.0.0.1:8787");
}
