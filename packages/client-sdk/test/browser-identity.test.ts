import assert from "node:assert/strict";
import { test } from "node:test";
import { browserDeviceName, browserName } from "../src/browser-identity";
import { Service } from "../src/service";
import type { ReignHubCache } from "../src/cache";
const chrome =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/142.0.0.0 Safari/537.36";
test("reports actual browser version and OS without misidentifying Edge or iOS", async () => {
  assert.equal(browserName(chrome), "Chrome 142.0.0.0 · macOS");
  assert.equal(
    browserName(chrome + " Edg/142.0.3595.53"),
    "Edge 142.0.3595.53 · macOS",
  );
  assert.equal(
    browserName(
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) Version/18.1 Mobile Safari/604.1",
    ),
    "Safari 18.1 · iOS 18.1",
  );
  assert.equal(
    browserName("Mozilla/5.0 (Android 15) Firefox/144.0"),
    "Firefox 144.0 · Android 15",
  );
  assert.equal(
    await browserDeviceName({
      userAgent: chrome,
      userAgentData: {
        getHighEntropyValues: async () => ({
          fullVersionList: [
            { brand: "Google Chrome", version: "142.0.7444.60" },
          ],
        }),
      },
    }),
    "Chrome 142.0.7444.60 · macOS",
  );
  assert.equal(
    await browserDeviceName({
      userAgent: chrome,
      userAgentData: {
        getHighEntropyValues: async () => {
          throw Error("blocked");
        },
      },
    }),
    "Chrome 142.0.0.0 · macOS",
  );
});
test("renames existing browser without replacing its pairing credentials and coalesces identity lookups", async () => {
  const old = {
    id: "device",
    connectionId: "123456",
    name: "Chrome",
    private: "private",
    public: "public",
    token: "token",
    service: "https://example.com",
  };
  let saved = old,
    requests = 0;
  const cache = {
    identities: {
      get: async () => saved,
      put: async (value: typeof old) => {
        saved = value;
      },
    },
  } as unknown as ReignHubCache;
  const service = new Service(old.service, cache);
  service.post = async (path, input, token) => {
    requests++;
    assert.equal(path, "/p2p/devices/presence");
    assert.equal(token, old.token);
    assert.equal(typeof (input as { name: string }).name, "string");
    return { ok: true } as never;
  };
  const [a, b] = await Promise.all([service.identity(), service.identity()]);
  assert.equal(a, b);
  assert.equal(requests, 1);
  assert.notEqual(a.name, "Chrome");
  assert.deepEqual({ ...a, name: "Chrome" }, old);
});
