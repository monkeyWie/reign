"use strict";
// Exercise installed producer methods on synthetic state. No IPC connection,
// model execution, real conversation or modification of the App is involved.
const fs = require("node:fs");
const vm = require("node:vm");
const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const bytes = fs.readFileSync(process.argv[2]);
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
      !asset.startsWith("/.vite/build/") ||
      !asset.endsWith(".js") ||
      entry.unpacked
    )
      continue;
    const offset = 8 + bytes.readUInt32LE(4) + Number(entry.offset);
    const source = bytes.subarray(offset, offset + entry.size).toString();
    const start = source.indexOf("resetAfterReconnect(e=!1){");
    if (start < 0) continue;
    const patch = source.indexOf("broadcastIpcStatePatches(e,t,r){", start);
    const snapshot = source.indexOf("broadcastConversationSnapshot(e){", patch);
    const end = source.indexOf("waitForConversationRevision(", snapshot);
    assert(
      patch > start && snapshot > patch && end > snapshot,
      "review changed stream producer",
    );
    assert(!found, "ambiguous stream producer");
    found = {
      asset,
      source,
      methods: [
        source.slice(start, patch),
        source.slice(patch, snapshot),
        source.slice(snapshot, end),
      ],
    };
  }
}
walk(header);
assert(found, "installed stream producer not found");
const producer = vm.runInNewContext(
  "({" + found.methods.join(",") + "})",
  {},
  { timeout: 1000 },
);
const frames = [];
Object.assign(producer, {
  streamRoles: new Map([["fixture", { role: "owner" }]]),
  streamRevisionByConversationId: new Map([["fixture", 500]]),
  streamRevisionWaiters: new Map(),
  patchPublications: 0,
  patchRecipients: 0,
  clearConversationStreamRoles() {
    this.streamRoles.clear();
  },
  getFollowerClientIds() {
    return ["fixture-follower"];
  },
  canBroadcastIpcStatePatches() {
    return true;
  },
  sendConversationSnapshot(id, targets, revision) {
    frames.push({ type: "snapshot", revision });
    return true;
  },
  params: {
    hostId: "local",
    transport: {
      sendPatches(id, host, targets, baseRevision, revision) {
        frames.push({ type: "patches", baseRevision, revision });
      },
    },
  },
});
producer.resetAfterReconnect();
assert.equal(producer.streamRevisionByConversationId.size, 0);
producer.streamRoles.set("fixture", { role: "owner" });
producer.broadcastConversationSnapshot("fixture");
producer.broadcastIpcStatePatches("fixture", [
  { op: "add", path: ["title"], value: "fixture" },
]);
assert.deepEqual(frames, [
  { type: "snapshot", revision: 1 },
  { type: "patches", baseRevision: 1, revision: 2 },
]);
console.log(
  JSON.stringify({
    passed: true,
    asset: found.asset,
    methodsSha256: createHash("sha256")
      .update(found.methods.join("\n"))
      .digest("hex"),
    beforeRevision: 500,
    frames,
    scope:
      "Installed producer resets revisions on backend reconnect; synthetic state only.",
  }),
);
