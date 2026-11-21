"use strict";
// Execute the installed renderer's failing text-input parser on synthetic data.
// Read package bytes only; never connect to an App, model or conversation.
const fs = require("node:fs");
const vm = require("node:vm");
const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const bytes = fs.readFileSync(process.argv[2]);
const inputs = JSON.parse(process.argv[3]);
const header = JSON.parse(bytes.subarray(16, 16 + bytes.readUInt32LE(12)));
let found;
function walk(node, path = "") {
  for (const [name, entry] of Object.entries(node.files ?? {})) {
    const asset = path + "/" + name;
    if (entry.files) {
      walk(entry, asset);
      continue;
    }
    if (
      !asset.startsWith("/webview/") ||
      !asset.endsWith(".js") ||
      entry.unpacked
    )
      continue;
    const offset = 8 + bytes.readUInt32LE(4) + Number(entry.offset);
    const source = bytes.subarray(offset, offset + entry.size).toString();
    const point = source.indexOf(
      'if("text"!==e.type||0===e.text_elements.length)return null;',
    );
    if (point < 0) continue;
    const start = source.lastIndexOf("function v(e){", point);
    const end = source.indexOf("function y(", point);
    assert(start >= 0 && end > start, "review changed upstream text parser");
    assert(!found, "ambiguous text parser");
    found = { asset, method: source.slice(start, end) };
  }
}
walk(header);
assert(found, "review changed installed renderer text parser");
const context = vm.createContext({
  d: { i: (text) => Buffer.byteLength(text) },
  p: "synthetic-app-input-prefix:",
});
const parse = vm.runInContext("(" + found.method + ")", context, {
  timeout: 1000,
});
assert.throws(
  () => parse({ type: "text", text: "before normalization" }),
  /reading 'length'/,
);
for (const input of inputs) assert.equal(parse(input), null);
console.log(
  JSON.stringify({
    passed: true,
    asset: found.asset,
    parserSha256: createHash("sha256").update(found.method).digest("hex"),
    scope:
      "Installed renderer parser; reproduced missing text_elements crash and validated normalized synthetic input. No live writes.",
  }),
);
