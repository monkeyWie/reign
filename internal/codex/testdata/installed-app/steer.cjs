"use strict";
// Read package bytes only. Execute the installed steer entry with synthetic
// state; intercept follower routing before any App/model/network operation.
const fs = require("node:fs");
const vm = require("node:vm");
const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const asar = fs.readFileSync(process.argv[2]);
const params = JSON.parse(process.argv[3]);
const header = JSON.parse(asar.subarray(16, 16 + asar.readUInt32LE(12)));
let found;
function walk(node, path = "") {
  for (const [name, entry] of Object.entries(node.files ?? {})) {
    const asset = path + "/" + name;
    if (entry.files) {
      walk(entry, asset);
      continue;
    }
    if (
      !asset.startsWith("/.vite/build/") ||
      !asset.endsWith(".js") ||
      entry.unpacked
    )
      continue;
    const offset = 8 + asar.readUInt32LE(4) + Number(entry.offset);
    const source = asar.subarray(offset, offset + entry.size).toString();
    const begin = source.indexOf("steerTurn(e,t,r,n,i,s,a,o,l,c,d){");
    if (begin < 0) continue;
    const end = source.indexOf("sendFollowUpMessage(", begin);
    assert(end > begin, "review changed upstream method boundary");
    assert(!found, "ambiguous upstream executor");
    found = { asset, method: source.slice(begin, end) };
  }
}
walk(header);
assert(found, "review changed upstream steer signature");
let captured;
const context = vm.createContext({
  Iv() {},
  bV: "synthetic owner",
  k5: async (value) => {
    captured = value;
    return { result: { synthetic: true } };
  },
});
const executor = vm.runInContext("({" + found.method + "})", context, {
  timeout: 1000,
});
const manager = {
  assertActive() {},
  getConversation: () => ({ latestCollaborationMode: null }),
  getConversationCwd: () => "/fixture",
  runtime: {},
  history: { createSearchIslandId: () => "fixture-id" },
  scheduler: {},
};
async function run(restore) {
  return executor.steerTurn.call(
    manager,
    params.conversationId,
    params.input,
    restore,
    params.serviceTier,
    params.attachments,
    params.clientUserMessageId,
  );
}
(async () => {
  await assert.rejects(
    run(undefined),
    /reading 'cwd'/,
    "must reproduce the reported missing-context failure",
  );
  const result = await run(params.restoreMessage);
  assert.equal(result.synthetic, true);
  assert.equal(captured.workspaceRoots[0], "/project");
  assert.equal(params.restoreMessage.id, params.clientUserMessageId);
  console.log(
    JSON.stringify({
      passed: true,
      asset: found.asset,
      executorSha256: createHash("sha256").update(found.method).digest("hex"),
      scope:
        "Installed steer entry + synthetic follower routing; no live App writes or model execution.",
    }),
  );
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
