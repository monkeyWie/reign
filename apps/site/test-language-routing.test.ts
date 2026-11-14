import { test } from "node:test";
import assert from "node:assert/strict";
import {
  withSiteLanguage,
  preferredLocale,
  publicLocalePath,
} from "./lib/language-routing";
const content = () =>
  new Response("<h1>English</h1>", {
    headers: { "Content-Type": "text/html" },
  });
const request = (path: string, headers: Record<string, string> = {}) =>
  new Request("https://reign.test" + path, { headers });

test("first visits negotiate browser language with quality weighting and a neutral English fallback", async () => {
  assert.equal(preferredLocale("fr,en;q=0.8,zh-CN;q=0.9"), "zh-CN");
  assert.equal(preferredLocale("zh;q=0,en;q=1"), "en");
  assert.equal(preferredLocale("fr-FR,de;q=0.9"), "en");
  const first = await withSiteLanguage(
    request("/docs/quick-start?from=site", {
      "Accept-Language": "zh-CN,en;q=0.8",
    }),
    content,
  );
  assert.equal(first.status, 307);
  assert.equal(
    first.headers.get("Location"),
    "https://reign.test/zh-CN/docs/quick-start?from=site",
  );
  assert.match(first.headers.get("Set-Cookie")!, /reign_locale=zh-CN/);
  assert.match(first.headers.get("Cache-Control")!, /no-store/);
  const english = await withSiteLanguage(request("/"), content);
  assert.equal(english.status, 200);
  assert.match(english.headers.get("Set-Cookie")!, /reign_locale=en/);
  assert.match(english.headers.get("Vary")!, /Cookie/);
});
test("manual language selection wins over browser language and persists across visits", async () => {
  const selection = await withSiteLanguage(
    request("/zh-CN/docs?lang=en&search=code", {
      "Accept-Language": "zh-CN",
      Cookie: "reign_locale=zh-CN",
    }),
    content,
  );
  assert.equal(selection.status, 303);
  assert.equal(
    selection.headers.get("Location"),
    "https://reign.test/docs?search=code",
  );
  assert.match(
    selection.headers.get("Set-Cookie")!,
    /reign_locale=en; Path=\/; Max-Age=31536000; SameSite=Lax; Secure/,
  );
  const next = await withSiteLanguage(
    request("/docs", { "Accept-Language": "zh-CN", Cookie: "reign_locale=en" }),
    content,
  );
  assert.equal(next.status, 200);
  assert.equal(next.headers.get("Location"), null);
  const chosenChinese = await withSiteLanguage(
    request("/", { "Accept-Language": "en", Cookie: "reign_locale=zh-CN" }),
    content,
  );
  assert.equal(chosenChinese.status, 307);
  assert.equal(
    chosenChinese.headers.get("Location"),
    "https://reign.test/zh-CN",
  );
});
test("explicit Chinese URLs stay available and language negotiation never touches application traffic or prefetches", async () => {
  const zh = await withSiteLanguage(
    request("/zh-CN/docs", {
      Cookie: "reign_locale=en",
      "Accept-Language": "en",
    }),
    content,
  );
  assert.equal(zh.status, 200);
  for (const path of [
    "/chat",
    "/account",
    "/admin",
    "/api/search",
    "/p2p/config",
    "/_next/static/a.js",
    "/og/en.png",
    "/unknown",
    "/en/docs",
  ]) {
    const response = await withSiteLanguage(
      request(path, { "Accept-Language": "zh-CN" }),
      content,
    );
    assert.equal(response.status, 200, path);
    assert.equal(response.headers.get("Set-Cookie"), null, path);
  }
  for (const header of [
    { RSC: "1" },
    { "Next-Router-Prefetch": "1" },
    { Purpose: "prefetch" },
  ]) {
    const response = await withSiteLanguage(
      request("/docs", { "Accept-Language": "zh-CN", ...header } as Record<
        string,
        string
      >),
      content,
    );
    assert.equal(response.status, 200);
    assert.equal(response.headers.get("Set-Cookie"), null);
  }
  assert.equal(publicLocalePath("/docs", "en"), "/docs");
  assert.equal(publicLocalePath("/zh-CN/docs", "en"), "/docs");
});
