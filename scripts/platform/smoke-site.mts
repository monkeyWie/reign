// Read-only smoke checks, reusable against local builds and deployed sites.
import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

export async function smokeSite(origin: string) {
  const pending = new Set(
    ["en", "zh-CN"].flatMap((lang) =>
      ["", "/features", "/technology", "/pricing", "/download", "/docs"].map(
        (path) => `/${lang}${path}`,
      ),
    ),
  );
  const visited = new Set<string>();
  let canonicalOrigin: string | undefined;
  while (pending.size) {
    const path = pending.values().next().value!;
    pending.delete(path);
    const response = await fetch(origin + path);
    assert.equal(response.status, 200, path);
    const html = await response.text();
    const lang = path.startsWith("/en") ? "en" : "zh-CN";
    assert.match(html, new RegExp(`<html[^>]*lang="${lang}"`), path);
    assert.match(html, /<h1[\s>]/, path);
    assert.doesNotMatch(
      html,
      /\[object Object\]/,
      `Invalid rendered content: ${path}`,
    );
    assert.match(html, /<title>[^<]+Reign|<title>Reign/, path);
    assert.match(html, /<meta name="description" content="[^\"]+"/, path);
    assert.match(
      html,
      new RegExp(
        `property="og:locale" content="${lang === "en" ? "en_US" : "zh_CN"}"`,
      ),
      path,
    );
    const canonical = html.match(/<link rel="canonical" href="([^"]+)"/);
    if (canonical) {
      canonicalOrigin = new URL(canonical[1]).origin;
      assert.equal(canonical[1], canonicalOrigin + path, path);
      for (const alternate of ["en", "zh-CN", "x-default"]) {
        const alternatePath = path.replace(
          /^\/(en|zh-CN)/,
          "/" + (alternate === "x-default" ? "en" : alternate),
        );
        assert.ok(
          html.includes(
            `hrefLang="${alternate}" href="${canonicalOrigin}${alternatePath}"`,
          ) ||
            html.includes(
              `hreflang="${alternate}" href="${canonicalOrigin}${alternatePath}"`,
            ),
          `Missing alternate ${alternate}: ${path}`,
        );
      }
      assert.match(
        html,
        /property="og:image" content="https?:\/\/[^\"]+\/og\/(en|zh-CN)\.png"/,
        path,
      );
    }
    if (path === "/en") {
      assert.match(html, /Beyond your desk/);
      assert.ok(
        !html.includes("Agent 在电脑上"),
        "English homepage must have translated content",
      );
    }
    if (/^\/(en|zh-CN)$/.test(path)) {
      for (const text of [
        "8.9",
        "39–48",
        "macOS",
        "Windows",
        "Linux",
        "iOS",
        "Android",
        "Web",
      ])
        assert.ok(
          html.includes(text),
          `Missing product highlight ${text}: ${path}`,
        );
    }
    const diagramKind =
      path.endsWith("/technology") || path.endsWith("/docs/architecture")
        ? "overview"
        : path.endsWith("/docs/architecture/stack")
          ? "stack"
          : path.endsWith("/docs/architecture/protocol") ||
              path.endsWith("/docs/guides/connections")
            ? "connection"
            : path.endsWith("/docs/agents/codex-app")
              ? "codex"
              : undefined;
    if (diagramKind)
      assert.ok(
        html.includes(`id="architecture-${diagramKind}-${lang}-title"`),
        `Missing localized architecture diagram: ${path}`,
      );
    assert.ok(
      !html.includes("/v2/") && !html.includes("Legacy Flutter"),
      `Internal migration history in public content: ${path}`,
    );
    visited.add(path);
    for (const match of html.matchAll(
      /href="(\/(?:en|zh-CN)\/docs(?:\/[^"?#]*)?)(?:[?#][^"]*)?"/g,
    )) {
      const linked = match[1];
      if (!visited.has(linked)) pending.add(linked);
    }
    // Localized prose must not send the reader back through legacy Chinese redirects.
    assert.ok(
      !/href="\/(?:docs|features|technology|pricing|download)(?:["/#?])/.test(
        html,
      ),
      `Unlocalized website link: ${path}`,
    );
  }
  for (const lang of ["en", "zh-CN"]) {
    for (const path of [
      "self-hosting/cloudflare",
      "contributing/development",
      "architecture/stack",
      "architecture/performance",
    ]) {
      assert.ok(
        visited.has(`/${lang}/docs/${path}`),
        `Unlinked translation: ${lang}/${path}`,
      );
    }
    assert.equal(
      (await fetch(`${origin}/${lang}/docs/not-a-reign-page`)).status,
      404,
    );
  }
  assert.equal((await fetch(origin + "/fr/docs")).status, 404);
  const root = await fetch(origin + "/", { redirect: "manual" });
  assert.ok([307, 308].includes(root.status));
  assert.equal(new URL(root.headers.get("location")!, origin).pathname, "/en");
  for (const path of ["/docs/quick-start", "/features"]) {
    const response = await fetch(origin + path, { redirect: "manual" });
    assert.equal(response.status, 308);
    assert.equal(
      new URL(response.headers.get("location")!, origin).pathname,
      "/zh-CN" + path,
    );
  }
  const search = await fetch(origin + "/api/search");
  assert.equal(search.status, 200);
  const index = await search.json();
  assert.equal(index.type, "advanced");
  assert.equal(index.i18n, true, "Browser search must filter by locale");
  const serialized = JSON.stringify(index);
  assert.match(serialized, /自建/);
  assert.match(serialized, /Self-hosting/);
  for (const path of visited) {
    if (path.includes("/docs"))
      assert.ok(serialized.includes(path), `Search index missing ${path}`);
  }
  for (const path of ["/chat", "/account", "/admin"]) {
    const html = await (await fetch(origin + path)).text();
    assert.match(html, /name="robots" content="noindex, nofollow"/, path);
  }
  const robots = await (await fetch(origin + "/robots.txt")).text();
  for (const path of ["/chat", "/account", "/admin", "/api/", "/p2p/"])
    assert.ok(robots.includes(`Disallow: ${path}`));
  const sitemap = await (await fetch(origin + "/sitemap.xml")).text();
  if (canonicalOrigin) {
    assert.ok(robots.includes(`Sitemap: ${canonicalOrigin}/sitemap.xml`));
    for (const path of visited)
      assert.ok(
        sitemap.includes(`<loc>${canonicalOrigin}${path}</loc>`),
        `Sitemap missing ${path}`,
      );
    assert.ok(sitemap.includes('hreflang="x-default"'));
  } else
    assert.ok(
      !sitemap.includes("<loc>"),
      "An unconfigured self-hosted site must not claim another domain",
    );
  for (const lang of ["en", "zh-CN"]) {
    const card = await fetch(`${origin}/og/${lang}.png`);
    assert.equal(card.status, 200);
    assert.match(card.headers.get("content-type") ?? "", /image\/png/);
  }
  console.log(
    `Bilingual website, ${visited.size - 10} linked docs, localized metadata/search, legacy redirects, noindex and sitemap passed`,
  );
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  await smokeSite(process.argv[2] || "http://127.0.0.1:8787");
