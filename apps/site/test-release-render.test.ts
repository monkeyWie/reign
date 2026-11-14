import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { ReleaseDownloads } from "./components/release-downloads";
import {
  parseReleases,
  releasesURL,
  type ReleaseCatalog,
} from "./lib/releases";

const tag = "v1.2.3";
const filenames = [
  "macos-arm64.dmg",
  "macos-x64.dmg",
  "windows-arm64-setup.exe",
  "windows-x64-setup.exe",
  "linux-arm64.deb",
  "linux-arm64.pkg.tar.gz",
  "linux-arm64.AppImage",
  "linux-x64.deb",
  "linux-x64.pkg.tar.gz",
  "linux-x64.AppImage",
  "android-arm64.apk",
  "ios-unsigned.ipa",
].map((suffix) => `reign-1.2.3-${suffix}`);
const catalog: ReleaseCatalog = {
  release: parseReleases([
    {
      tag_name: tag,
      draft: false,
      prerelease: false,
      published_at: "2026-11-14T00:00:00Z",
      assets: filenames.map((name) => ({
        name,
        size: 9 * 1024 * 1024,
        browser_download_url: `${releasesURL}/download/${tag}/${name}`,
      })),
    },
  ]),
  checkedAt: "2026-11-14T01:00:00Z",
  stale: false,
};

test("English and Chinese download HTML includes every native link before JavaScript runs", () => {
  for (const lang of ["en", "zh-CN"] as const) {
    const html = renderToStaticMarkup(
      createElement(ReleaseDownloads, { lang, catalog }),
    );
    assert.ok(html.includes("Reign 1.2.3"));
    for (const name of filenames)
      assert.ok(html.includes(`href="${releasesURL}/download/${tag}/${name}"`));
    assert.doesNotMatch(html, /正在获取|正在加载|Loading|Fetching|aria-busy/);
  }
});

test("stale snapshots still render downloads; an unavailable catalogue provides a GitHub fallback", () => {
  const stale = renderToStaticMarkup(
    createElement(ReleaseDownloads, {
      lang: "en",
      catalog: { ...catalog, stale: true },
    }),
  );
  assert.ok(stale.includes("Reign 1.2.3"));
  const unavailable = renderToStaticMarkup(
    createElement(ReleaseDownloads, {
      lang: "en",
      catalog: { release: null, checkedAt: null, stale: true },
    }),
  );
  assert.ok(unavailable.includes(`href="${releasesURL}"`));
  assert.doesNotMatch(unavailable, /Loading|Fetching/);
});
