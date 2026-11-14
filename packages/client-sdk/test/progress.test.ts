import assert from "node:assert/strict";
import { test } from "node:test";
import { progressStep, recordProgress } from "../src/progress";
test("connection trace updates node outcomes in place and bounds history", () => {
  const start = progressStep(
    "stun:cf",
    "udp",
    "公共 STUN 节点",
    "stun:stun.cloudflare.com:3478",
    "info",
  );
  const initial = [start];
  assert.equal(
    recordProgress(initial, { ...start, at: start.at + 50 }),
    initial,
  );
  const updated = recordProgress(initial, {
    ...start,
    message: "STUN 节点已返回公网映射",
    state: "success",
    at: start.at + 100,
  });
  assert.equal(updated.length, 1);
  assert.equal(updated[0].at, start.at);
  assert.equal(updated[0].state, "success");
  let rows = updated;
  for (let i = 0; i < 200; i++)
    rows = recordProgress(rows, progressStep(String(i), "udp", "候选地址"));
  assert.equal(rows.length, 80);
  assert.equal(initial[0].state, "info");
});
