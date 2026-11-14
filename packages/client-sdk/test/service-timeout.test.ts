import assert from "node:assert/strict";
import { test } from "node:test";
import { Service } from "../src/service";
import type { ReignHubCache } from "../src/cache";

test("each HTTP step gets a fresh two-minute deadline and remains cancellable", async () => {
  const originalFetch = globalThis.fetch;
  const originalTimeout = AbortSignal.timeout;
  const deadlines: number[] = [];
  const signals: AbortSignal[] = [];
  try {
    AbortSignal.timeout = (duration) => {
      deadlines.push(duration);
      return originalTimeout(duration);
    };
    globalThis.fetch = async (_url, options) => {
      signals.push(options!.signal!);
      if (signals.length <= 2) return Response.json({ ok: true });
      return new Promise((_resolve, reject) => {
        const signal = options!.signal!;
        if (signal.aborted) reject(signal.reason);
        else
          signal.addEventListener("abort", () => reject(signal.reason), {
            once: true,
          });
      });
    };
    const api = new Service("https://example.com", {} as ReignHubCache);
    await api.post("/p2p/devices/resolve");
    await api.post("/p2p/config");
    assert.notEqual(signals[0], signals[1]);
    const controller = new AbortController();
    const pending = api.post(
      "/p2p/devices/resolve",
      {},
      undefined,
      controller.signal,
    );
    controller.abort(new Error("用户取消"));
    await assert.rejects(pending, /用户取消/);
    assert.deepEqual(deadlines, [120000, 120000, 120000]);
    assert.equal(signals[0].aborted, false);
    assert.equal(signals[1].aborted, false);
  } finally {
    globalThis.fetch = originalFetch;
    AbortSignal.timeout = originalTimeout;
  }
});
