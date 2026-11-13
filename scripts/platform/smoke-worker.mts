// Run against an isolated local Worker with its D1 migrations applied.
// The spawned host only exposes a fake Agent, never real local sessions.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { Service } from "../../packages/client-sdk/src/service";
import { Connection } from "../../packages/client-sdk/src/connection";
import type { ReignCache } from "../../packages/client-sdk/src/cache";

const origin = process.argv[2] || "http://127.0.0.1:8787";
const table = () => {
  const rows = new Map<string, unknown>();
  return {
    get: async (key: unknown) => rows.get(JSON.stringify(key)),
    put: async (row: Record<string, unknown>) => {
      rows.set(
        JSON.stringify(row.id ? [row.service, row.id] : row.service),
        row,
      );
      // Identity tables use the service key.
      if (row.token) rows.set(JSON.stringify(row.service), row);
    },
  };
};
const cache = {
  identities: table(),
  devices: table(),
} as unknown as ReignCache;
const child = spawn("go", ["run", "./testsupport/workerfixture", origin], {
  cwd: fileURLToPath(new URL("../../clients/nsgo", import.meta.url)),
  detached: true,
  stdio: ["ignore", "pipe", "pipe"],
});
let stderr = "";
child.stderr.on("data", (b) => (stderr += b));
let client: Connection | undefined;
try {
  const code = await new Promise<string>((resolve, reject) => {
    const timer = setTimeout(
      () => reject(Error("Fixture timed out: " + stderr)),
      60000,
    );
    let out = "";
    child.stdout.on("data", (b) => {
      out += b;
      const m = out.match(/"connectionId":"(\d{6})"/);
      if (m) {
        clearTimeout(timer);
        resolve(m[1]);
      }
    });
    child.on("exit", (n) => {
      clearTimeout(timer);
      reject(Error(`Fixture exited ${n}: ${stderr}`));
    });
  });
  for (const path of ["/", "/docs", "/chat", "/account"]) {
    const response = await fetch(origin + path);
    assert.equal(response.status, 200, path);
    assert.match(await response.text(), /Reign/);
  }
  const service = new Service(origin, cache);
  const config = await service.config();
  assert.equal(config.deployment, "self-hosted");
  assert.ok(config.grantPublicKey);
  const saved = await service.pair(code);
  client = await new Connection(
    saved,
    await service.identity(),
    config,
  ).connect();
  const providers = await client.call<{ id: string }[]>({
    method: "providers",
  });
  assert.ok(providers.some((p) => p.id === "fixture"));
  const page = await client.call<{ sessions: { id: string }[] }>({
    method: "sessions",
    provider: "fixture",
  });
  assert.equal(page.sessions[0].id, "isolated");
  await client.call({
    method: "send",
    provider: "fixture",
    session: "isolated",
    text: "Worker ↔ Go signed-lease smoke test",
  });
  const transcript = await client.call({
    method: "transcript",
    provider: "fixture",
    session: "isolated",
  });
  assert.match(JSON.stringify(transcript), /signed-lease smoke test/);
  console.log(
    "Worker pages, D1 identity, encrypted consent pairing, signed host access, sessions and send passed",
  );
} finally {
  client?.close();
  if (child.pid) {
    try {
      process.kill(-child.pid, "SIGTERM");
    } catch {}
  }
  child.stdout.destroy();
  child.stderr.destroy();
}
