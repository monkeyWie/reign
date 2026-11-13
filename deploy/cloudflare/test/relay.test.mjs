import { before, after, test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { once } from "node:events";
import WebSocket from "ws";

const token = "agenthub-local-test-token-0123456789abcdef";
const origin = "http://127.0.0.1:18787";
const sockets = new Set();
let worker, stateDir, logs = "";
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
function run(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { ...options, stdio: ["ignore", "pipe", "pipe"] });
    let output = "";
    child.stdout.on("data", (b) => { output += b; });
    child.stderr.on("data", (b) => { output += b; });
    child.on("error", reject);
    child.on("exit", (code) => code === 0 ? resolve(output) : reject(new Error(output)));
  });
}
before(async () => {
  stateDir = await mkdtemp(join(tmpdir(), "agenthub-worker-test-"));
  worker = spawn(process.execPath, [
    "node_modules/wrangler/bin/wrangler.js", "dev", "--local", "--ip", "127.0.0.1",
    "--port", "18787", "--persist-to", stateDir, "--var", `AGENTHUB_RELAY_TOKEN:${token}`,
  ], { env: { ...process.env, WRANGLER_SEND_METRICS: "false" }, stdio: ["ignore", "pipe", "pipe"] });
  const collect = (data) => { logs = (logs + data).slice(-64000); };
  worker.stdout.on("data", collect); worker.stderr.on("data", collect);
  let ready = false;
  for (let i = 0; i < 100; i++) {
    if (worker.exitCode !== null) throw new Error(logs);
    try { if ((await fetch(`${origin}/healthz`)).status === 200) { ready = true; break; } } catch {}
    await delay(100);
  }
  assert.ok(ready, `Worker did not start:\n${logs}`);
}, { timeout: 20000 });
after(async () => {
  for (const socket of sockets) socket.terminate();
  if (worker && worker.exitCode === null) {
    const stopped = once(worker, "exit"); worker.kill("SIGTERM");
    await Promise.race([stopped, delay(5000)]);
    if (worker.exitCode === null) { worker.kill("SIGKILL"); await stopped; }
  }
  if (stateDir) await rm(stateDir, { recursive: true, force: true });
});

class Peer {
  constructor(socket) {
    this.ws = socket; this.frames = []; this.waiters = [];
    socket.on("message", (raw) => {
      const frame = JSON.parse(raw.toString());
      if (this.waiters.length) this.waiters.shift()(frame); else this.frames.push(frame);
    });
  }
  next() {
    if (this.frames.length) return Promise.resolve(this.frames.shift());
    return new Promise((resolve, reject) => {
      const complete = (frame) => { clearTimeout(timer); resolve(frame); };
      const timer = setTimeout(() => { this.waiters = this.waiters.filter((fn) => fn !== complete); reject(new Error("frame timeout")); }, 3000);
      this.waiters.push(complete);
    });
  }
  send(frame) { this.ws.send(JSON.stringify(frame)); }
}
async function connect(role, hostID) {
  const ws = new WebSocket(`${origin.replace("http", "ws")}/relay?role=${role}&id=${hostID}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  sockets.add(ws); ws.on("close", () => sockets.delete(ws));
  const peer = new Peer(ws);
  await once(ws, "open");
  const welcome = await peer.next();
  assert.equal(welcome.kind, "welcome"); assert.match(welcome.peer, /^[a-f0-9]{32}$/);
  peer.id = welcome.peer;
  return peer;
}

test("edge auth, Origin and upgrade checks", async () => {
  const endpoint = `${origin}/relay?role=host&id=${"a".repeat(32)}`;
  assert.equal((await fetch(endpoint)).status, 401);
  assert.equal((await fetch(endpoint, { headers: { Authorization: `Bearer ${token}`, Origin: "https://untrusted.invalid" } })).status, 403);
  assert.equal((await fetch(endpoint, { headers: { Authorization: `Bearer ${token}` } })).status, 426);
});

test("browser subprotocol authentication checks Origin and never echoes credentials", async () => {
  const hostID = "e".repeat(32);
  const host = await connect("host", hostID);
  const auth = `auth.${Buffer.from(token).toString("base64url")}`;
  const ws = new WebSocket(`${origin.replace("http", "ws")}/relay?role=client&id=${hostID}`,
    ["agenthub.v1", auth], { origin });
  sockets.add(ws); ws.on("close", () => sockets.delete(ws));
  const client = new Peer(ws);
  await once(ws, "open");
  assert.equal(ws.protocol, "agenthub.v1");
  const welcome = await client.next();
  assert.equal(welcome.kind, "welcome");
  client.send({kind:"hello", body:"YWJj"});
  assert.deepEqual(await host.next(), {peer:welcome.peer, kind:"hello", body:"YWJj"});
  const endpoint = `${origin}/relay?role=client&id=${hostID}`;
  assert.equal((await fetch(endpoint, {headers:{Origin:"https://untrusted.invalid", "Sec-WebSocket-Protocol":`agenthub.v1, ${auth}`}})).status, 403);
  assert.equal((await fetch(endpoint, {headers:{Origin:origin, "Sec-WebSocket-Protocol":"agenthub.v1, auth.d3Jvbmc"}})).status, 401);
  assert.equal((await fetch(`${origin}/relay?role=host&id=${hostID}`, {headers:{Origin:origin, Authorization:`Bearer ${token}`}})).status, 403);
  ws.close(); host.ws.close();
});

test("host rooms isolate traffic and client identities cannot be forged", async () => {
  const hostA = await connect("host", "a".repeat(32));
  const hostB = await connect("host", "b".repeat(32));
  const clientA = await connect("client", "a".repeat(32));
  const otherA = await connect("client", "a".repeat(32));
  const clientB = await connect("client", "b".repeat(32));
  const opaque = Buffer.from("opaque encrypted payload").toString("base64");
  clientA.send({ peer: otherA.id, kind: "hello", body: opaque });
  assert.deepEqual(await hostA.next(), { peer: clientA.id, kind: "hello", body: opaque });
  hostA.send({ peer: clientB.id, kind: "chunk", body: opaque });
  hostB.send({ peer: clientB.id, kind: "chunk", body: opaque });
  assert.deepEqual(await clientB.next(), { peer: clientB.id, kind: "chunk", body: opaque });
  assert.equal(clientB.frames.length, 0);
  hostA.send({ peer: otherA.id, kind: "signal", body: opaque });
  assert.deepEqual(await otherA.next(), { peer: otherA.id, kind: "signal", body: opaque });
  clientA.ws.close();
  assert.deepEqual(await hostA.next(), { peer: clientA.id, kind: "bye" });
  for (const peer of [hostA, hostB, otherA, clientB]) peer.ws.close();
});

test("host disconnect closes its clients and permits a fresh host generation", async () => {
  const hostID = "c".repeat(32);
  const host = await connect("host", hostID), client = await connect("client", hostID);
  const closed = once(client.ws, "close"); host.ws.close();
  assert.equal((await closed)[0], 1013);
  const replacement = await connect("host", hostID), replacementClient = await connect("client", hostID);
  replacementClient.send({ kind: "hello", body: "YWJj" });
  assert.equal((await replacement.next()).peer, replacementClient.id);
  replacement.ws.close(); replacementClient.ws.close();
});

test("invalid binary messages terminate just that client", async () => {
  const host = await connect("host", "d".repeat(32)), client = await connect("client", "d".repeat(32));
  const closed = once(client.ws, "close"); client.ws.send(Buffer.from("invalid"));
  assert.equal((await closed)[0], 1003);
  assert.equal((await host.next()).kind, "bye"); host.ws.close();
});

test("native Go clients use encrypted relay, real P2P, chunking, ping and fallback", async () => {
  const output = await run("go", ["test", "-race", "./internal/remote", "-run", "^TestRelayAndP2P$", "-count=1", "-v"], {
    cwd: resolve("../.."),
    env: { ...process.env, AGENTHUB_TEST_RELAY_URL: origin.replace("http", "ws"), AGENTHUB_TEST_RELAY_TOKEN: token },
  });
  assert.match(output, /active path: Relay/); assert.match(output, /active path: P2P/);
  console.log(output.trim());
}, { timeout: 60000 });

test("Flutter client exchanges encrypted RPC through the Worker", {skip: process.env.AGENTHUB_TEST_FLUTTER !== "1", timeout: 120000}, async () => {
  const output = await run("python3", ["scripts/test-flutter-relay.py", "--native-only"], {
    cwd: resolve("../.."),
    env: {...process.env, AGENTHUB_TEST_RELAY_URL:origin.replace("http","ws"), AGENTHUB_TEST_RELAY_TOKEN:token},
  });
  assert.match(output, /All tests passed/);
});
