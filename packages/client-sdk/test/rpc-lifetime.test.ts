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
  const sent: { id: string }[] = [];
  Reflect.set(c, "send", async (raw: Uint8Array) => {
    sent.push(JSON.parse(new TextDecoder().decode(raw)));
  });
  const reply = (id: string, result: unknown) =>
    Reflect.get(c, "response").call(c, utf8(JSON.stringify({ id, result })));
  return { c, sent, reply };
}

test("business calls survive minutes of waiting; explicit setup deadlines still expire", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { c, sent, reply } = fixture();
  try {
    let settled = false;
    const result = c.call({ method: "transcript" }).finally(() => {
      settled = true;
    });
    await Promise.resolve();
    await Promise.resolve();
    t.mock.timers.tick(10 * 60 * 1000);
    await Promise.resolve();
    assert.equal(settled, false);
    reply(sent[0].id, { turns: ["large snapshot"] });
    assert.deepEqual(await result, { turns: ["large snapshot"] });

    const verify = c.call({ method: "providers" }, 120000);
    const rejected = assert.rejects(verify, /请求超时/);
    t.mock.timers.tick(120001);
    await rejected;
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
  assert.equal(sent.length, 0);
  const pending = c.call({ method: "sessions" });
  c.close("连接已断开");
  await assert.rejects(pending, /连接已断开/);
  assert.equal(Reflect.get(c, "pending").size, 0);
});

test("a slow fragmented response stays valid while chunks keep arriving", async (t) => {
  t.mock.timers.enable({ apis: ["Date"] });
  const { c, sent } = fixture();
  const result = c.call({ method: "transcript" });
  await Promise.resolve();
  await Promise.resolve();
  const raw = utf8(JSON.stringify({ id: sent[0].id, result: "slow snapshot" }));
  const assemble = Reflect.get(c, "assemble").bind(c);
  const part = Math.ceil(raw.length / 3);
  for (let i = 0; i < 3; i++) {
    if (i) t.mock.timers.tick(90000);
    assemble(
      utf8(
        JSON.stringify({
          id: "snapshot",
          i,
          n: 3,
          b: b64(raw.slice(i * part, (i + 1) * part)),
        }),
      ),
    );
  }
  assert.equal(await result, "slow snapshot");
  c.close();
});
