import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  cpSync,
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { gzipSync } from "node:zlib";

const root = fileURLToPath(new URL("../../", import.meta.url));
const output = join(root, "build/nsgo-console");
const assets = join(root, "apps/site/public/assets/chat");
const version = process.env.VERSION || "0.1.0";
mkdirSync(output, { recursive: true });
const result = spawnSync(
  "sh",
  ["scripts/build.sh", "web", "-output", output, "-version", version],
  {
    cwd: join(root, "clients/nsgo"),
    stdio: "inherit",
    env: process.env,
  },
);
if (result.status !== 0) process.exit(result.status ?? 1);
const built = join(output, `ReignHub-${version}-web`);
if (!existsSync(join(built, "nsgo.json")))
  throw new Error("NSGo did not produce the Web manifest");
const config = JSON.parse(readFileSync(join(built, "nsgo.json"), "utf8"));
rmSync(assets, { recursive: true, force: true });
mkdirSync(assets, { recursive: true });
for (const file of readdirSync(built)) {
  if (["index.html", config.wasm, "manifest.webmanifest"].includes(file))
    continue;
  cpSync(join(built, file), join(assets, file), { recursive: true });
}
const wasm = readFileSync(join(built, config.wasm));
config.wasm += ".gz";
const compressed = gzipSync(wasm, { level: 9 });
config.wasmSize = compressed.length;
writeFileSync(join(assets, config.wasm), compressed);
// Small immutable objects work on CF Assets even when Range is ignored.
// Keep the complete gzip available for tooling and verify assembly in smoke tests.
config.wasmParts = [];
const partSize = 512 * 1024;
for (let start = 0, i = 0; start < compressed.length; start += partSize, i++) {
  const part = compressed.subarray(start, start + partSize);
  const file = config.wasm.replace(
    /\.wasm\.gz$/,
    `.part-${String(i).padStart(3, "0")}.bin`,
  );
  writeFileSync(join(assets, file), part);
  config.wasmParts.push({ file, size: part.length });
}
writeFileSync(join(assets, "nsgo.json"), JSON.stringify(config) + "\n");
const css = readFileSync(
  join(root, "apps/site/components/console-loading.css"),
);
const cssFile =
  "loading." +
  createHash("sha256").update(css).digest("hex").slice(0, 10) +
  ".css";
writeFileSync(join(assets, cssFile), css);
const downloader = readFileSync(join(root, "clients/nsgo/web/download.mjs"));
const downloaderFile =
  "download." +
  createHash("sha256").update(downloader).digest("hex").slice(0, 10) +
  ".mjs";
writeFileSync(join(assets, downloaderFile), downloader);
const html = readFileSync(join(root, "clients/nsgo/web/index.html"), "utf8")
  .replace("__LOADER__", config.loader)
  .replace("__DOWNLOADER__", downloaderFile)
  .replace("__LOADING_CSS__", cssFile)
  .replace(
    "__NSGO_CONFIG__",
    JSON.stringify(config).replaceAll("<", "\\u003c"),
  );
writeFileSync(join(assets, "index.html"), html);
const headers = readdirSync(assets)
  .filter((file) => /\.[0-9a-f]{10}\./.test(file))
  .map(
    (file) =>
      `/assets/chat/${file}\n  Cache-Control: public, max-age=31536000, immutable\n`,
  )
  .join("\n");
writeFileSync(
  join(root, "apps/site/public/_headers"),
  headers +
    "\n/assets/chat/nsgo.json\n  Cache-Control: no-cache\n\n/assets/chat/index.html\n  Cache-Control: no-cache\n",
);
for (const file of readdirSync(assets)) {
  if (statSync(join(assets, file)).size > 25 * 1024 * 1024)
    throw new Error(`CF asset exceeds 25 MiB: ${file}`);
}
console.log(
  `NSGo console: ${(wasm.length / 1048576).toFixed(2)} MiB module, ${(compressed.length / 1048576).toFixed(2)} MiB download`,
);
