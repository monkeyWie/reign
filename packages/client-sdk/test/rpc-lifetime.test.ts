import assert from "node:assert/strict";
import { test } from "node:test";
import { Connection } from "../src/connection";
import { b64, utf8 } from "../src/crypto";
import type { Identity, ServiceConfig } from "@reignhub/protocol";
import type { SavedDevice } from "../src/cache";

function fixture() {
  const c = new Connection(
    {} as SavedDevice,
    {} as Identity,
    {} as ServiceConfig,
  );
  const sent: { id: string; method: string; cancel?: string }[] = [];
  Reflect.set(
    c,
    "send",
    async (
      raw: Uint8Array,
      _id: string,
      _method: string,
      active = () => true,
    ) => {
      await Promise.resolve();
      if (active()) sent.push(JSON.parse(new TextDecoder().decode(raw)));
    },
  );
  const reply = (id: string, result: unknown) =>
    Reflect.get(c, "response").call(c, utf8(JSON.stringify({ id, result })));
  return { c, sent, reply };
}

test("silent reads expire after 30 seconds; explicit setup deadlines stay absolute", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { c, sent } = fixture();
  try {
    const stalled = c.call({ method: "transcript" });
    const rejected = assert.rejects(stalled, /30 秒没有进展/);
    await Promise.resolve();
    await Promise.resolve();
    t.mock.timers.tick(30001);
    await rejected;
    await Promise.resolve();
    await Promise.resolve();
    assert.ok(
      sent.some(
        (r) => r.method === "request.cancel" && r.cancel === sent[0].id,
      ),
    );
    assert.equal(Reflect.get(c, "closed"), false);
    const verify = c.call({ method: "providers" }, 1000);
    const timedOut = assert.rejects(verify, /请求超时/);
    t.mock.timers.tick(1001);
    await timedOut;
  } finally {
    c.close();
  }
});

test("cancellation stops a queued read; connection loss rejects outstanding calls", async () => {
  const { c, sent } = fixture();
  const controller = new AbortController();
  const canceled = c.call(
    { method: "transcript" },
    undefined,
    controller.signal,
  );
  controller.abort(new Error("切换会话"));
  await assert.rejects(canceled, /切换会话/);
  assert.equal(sent.filter((r) => r.method !== "request.cancel").length, 0);
  const pending = c.call({ method: "sessions" });
  c.close("连接已断开");
  await assert.rejects(pending, /连接已断开/);
  assert.equal(Reflect.get(c, "pending").size, 0);
});

test("fragment progress keeps a slow read alive beyond 30 seconds", async (t) => {
  t.mock.timers.enable({ apis: ["Date", "setTimeout"] });
  const { c, sent } = fixture();
  try {
    const result = c.call({ method: "transcript" });
    await Promise.resolve();
    await Promise.resolve();
    const request = sent[0].id;
    const raw = utf8(JSON.stringify({ id: request, result: "slow snapshot" }));
    const assemble = Reflect.get(c, "assemble").bind(c);
    const part = Math.ceil(raw.length / 3);
    for (let i = 0; i < 3; i++) {
      if (i) t.mock.timers.tick(20000);
      await assemble(
        utf8(
          JSON.stringify({
            r: request,
            e: "identity",
            s: raw.length,
            id: "snapshot",
            i,
            n: 3,
            b: b64(raw.slice(i * part, (i + 1) * part)),
          }),
        ),
      );
    }
    assert.equal(await result, "slow snapshot");
  } finally {
    c.close();
  }
});
