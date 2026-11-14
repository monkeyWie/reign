import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { Connection } from "../src/connection";
import { Cipher, envelope, keypair, sum } from "../src/crypto";
import type { Identity, ServiceConfig } from "@reignhub/protocol";
import type { SavedDevice } from "../src/cache";
test("browser SDK and Go host share handshake, AEAD, fragmentation, conditional snapshots", async () => {
  const cwd = fileURLToPath(new URL("../../../clients/nsgo", import.meta.url));
  const child = spawn("go", ["run", "./testsupport/fixture"], {
    cwd,
    detached: true,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let stderr = "";
  child.stderr.on("data", (b) => (stderr += b.toString()));
  try {
    const origin = await new Promise<string>((resolve, reject) => {
      let out = "";
      const timer = setTimeout(
        () => reject(Error(stderr || "Go fixture startup timed out")),
        60000,
      );
      child.stdout.on("data", (b) => {
        out += b.toString();
        const m = out.match(/http:\/\/127\.0\.0\.1:\d+/);
        if (m) {
          clearTimeout(timer);
          resolve(m[0]);
        }
      });
      child.on("exit", (code) => {
        clearTimeout(timer);
        reject(Error("Go fixture exited " + code + ": " + stderr));
      });
    });
    const r = await fetch(origin + "/fixture");
    const f = (await r.json()) as {
      Relay: string;
      Host: string;
      Key: string;
      Token: string;
    };
    const saved = {
      id: f.Host,
      key: f.Key,
      service: origin,
      connectionId: "123456",
      name: "Fixture",
      public: "ab".repeat(32),
      host: true,
      seen: 0,
      expires: 0,
      identityId: "fixture-client",
    } satisfies SavedDevice;
    const identity = {
      id: "fixture-client",
      token: f.Token,
      name: "Browser",
      private: "",
      public: "",
      service: origin,
      connectionId: "000000",
    } satisfies Identity;
    const config = {
      protocol: 1,
      publicStun: [],
      selfStun: [],
      tcpStun: [],
      deployment: "self-hosted",
      accountRequired: false,
      loginProviders: [],
      grantPublicKey: "",
      serviceId: "",
    } satisfies ServiceConfig;
    const steps: { id: string; message: string; detail?: string }[] = [];
    const client = await new Connection(saved, identity, config, (step) =>
      steps.push(step),
    ).connect();
    assert.ok(
      steps.some(
        (step) =>
          step.id === "relay-ready" && step.detail?.includes("/p2p/relay"),
      ),
    );
    assert.ok(
      steps.some(
        (step) => step.id === "handshake" && step.message.includes("完成"),
      ),
    );
    assert.ok(
      steps.some(
        (step) => step.id === "path:relay" && step.message.includes("中继"),
      ),
    );
    assert.ok(!JSON.stringify(steps).includes(f.Token));
    assert.ok(!JSON.stringify(steps).includes(f.Key));
    try {
      assert.match(client.path, /Relay/);
      for (let n = 0; n < 2; n++) {
        const page = await client.call<{
          sessions: { id: string; preview: string }[];
        }>({ method: "sessions", provider: "fixture" });
        assert.equal(page.sessions[0].id, "isolated");
        assert.equal(page.sessions[0].preview, "fixture🙂".repeat(20000));
      }
    } finally {
      client.close();
    }
  } finally {
    if (child.pid)
      try {
        process.kill(-child.pid, "SIGTERM");
      } catch {}
    child.stdout.destroy();
    child.stderr.destroy();
  }
});
test("X25519 pairing envelopes bind identities and context", async () => {
  const a = keypair(),
    b = keypair();
  const sealed = (await envelope(
    a.private,
    b.public,
    "reignhub/pair/request/test",
    { remember: true },
  )) as string;
  assert.deepEqual(
    await envelope(
      b.private,
      a.public,
      "reignhub/pair/request/test",
      undefined,
      sealed,
    ),
    { remember: true },
  );
  await assert.rejects(
    envelope(
      b.private,
      a.public,
      "reignhub/pair/reply/test",
      undefined,
      sealed,
    ),
  );
});
test("AEAD rejects duplicate data and authenticated-context substitution", async () => {
  const secret = await sum("test pairing secret");
  const a = await Cipher.create(secret, 1),
    b = await Cipher.create(secret, 2);
  const sealed = await a.seal(
    "reign/p2p/test/c2h/chunk",
    new TextEncoder().encode("message"),
  );
  assert.equal(
    new TextDecoder().decode(await b.open("reign/p2p/test/c2h/chunk", sealed)),
    "message",
  );
  await assert.rejects(b.open("reign/p2p/test/c2h/chunk", sealed));
  const other = await a.seal("hello", new Uint8Array([1]));
  await assert.rejects(b.open("chunk", other));
});
