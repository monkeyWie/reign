// Fail CI for missing translations before publishing an untranslated page.
import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";
const root = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../apps/site",
);
const english = JSON.parse(
  readFileSync(resolve(root, "lib/locales/en.json"), "utf8"),
);
function files(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory()
      ? files(resolve(dir, entry.name))
      : [resolve(dir, entry.name)],
  );
}
for (const file of [
  ...files(resolve(root, "app")),
  ...files(resolve(root, "components")),
].filter((path) => path.endsWith(".tsx"))) {
  const tree = ts.createSourceFile(
    file,
    readFileSync(file, "utf8"),
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
  function visit(node) {
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      node.expression.text === "t" &&
      node.arguments[0] &&
      ts.isStringLiteral(node.arguments[0])
    ) {
      const key = node.arguments[0].text;
      assert.ok(
        Object.hasOwn(english, key) && english[key].trim(),
        `Missing English copy in ${file}: ${key}`,
      );
    }
    ts.forEachChild(node, visit);
  }
  visit(tree);
}
const docs = files(resolve(root, "content/docs")).filter(
  (file) => file.endsWith(".mdx") || file.endsWith(".json"),
);
for (const file of docs) {
  const twin = file.includes(".zh-CN.")
    ? file.replace(".zh-CN.", ".")
    : file.replace(/\.(mdx|json)$/, ".zh-CN.$1");
  assert.ok(existsSync(twin), `Missing language variant: ${twin}`);
  if (file.endsWith(".json") && !file.includes(".zh-CN.")) {
    assert.deepEqual(
      JSON.parse(readFileSync(file, "utf8")).pages,
      JSON.parse(readFileSync(twin, "utf8")).pages,
      `Sidebar slugs differ: ${file}`,
    );
  }
}
console.log(
  "Marketing translations and bilingual documentation coverage passed",
);
