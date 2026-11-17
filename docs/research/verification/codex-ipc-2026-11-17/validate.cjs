'use strict';

// Offline harness. Only the installed App's registration/forwarding functions
// are evaluated. The application executor is synthetic and never calls Codex.
const fs = require('node:fs');
const path = require('node:path');
const net = require('node:net');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { randomUUID } = require('node:crypto');

const root = __dirname;
// Pinned offline fixture: read installed package bytes only. No live App access.
const { createHash } = require('node:crypto');
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'manifest.json'), 'utf8'));
const asarPath = process.argv[2] || '/Applications/ChatGPT.app/Contents/Resources/app.asar';
const asar = fs.readFileSync(asarPath);
const header = JSON.parse(asar.subarray(16, 16 + asar.readUInt32LE(12)).toString('utf8'));
let entry = header;
for (const part of manifest.sourceAsset.replace(/^app-source\//, '').split('/')) {
  entry = entry.files?.[part];
  assert(entry, 'pinned App asset missing: re-inspect the new package before updating fixture');
}
assert(!entry.unpacked && !entry.link, 'expected packed asset');
const offset = 8 + asar.readUInt32LE(4) + Number(entry.offset);
const source = asar.subarray(offset, offset + entry.size);
assert.equal(createHash('sha256').update(source).digest('hex'), manifest.sourceSha256,
  'installed App changed; fixture needs review');
const sourceText = source.toString('utf8');
const begin = sourceText.indexOf('function cQ(');
const end = sourceText.indexOf('function cJ(', begin);
assert(begin >= 0 && end > begin, 'pinned registration boundaries missing');
const registration = sourceText.slice(begin, end);
assert.equal(createHash('sha256').update(registration).digest('hex'), manifest.extractedSha256);

const context = vm.createContext({ setTimeout, clearTimeout, Symbol, cH: 1000, cG: new WeakMap() });
vm.runInContext(registration, context, { timeout: 1000 });
const handlers = new Map();
const fakeThread = '00000000-0000-4000-8000-000000000001';
const forwarded = [];
const ipc = { addRequestHandler(method, canHandle, handle) {
  assert(!handlers.has(method));
  handlers.set(method, { canHandle, handle });
  return () => handlers.delete(method);
} };
const dispose = context.cQ({ hostId: 'local', ipcClient: ipc,
  canHandle: async (params, host) => host === 'local' && params.conversationId === fakeThread,
  discoverOwner: async id => id === fakeThread,
  requestThreadFollower: async value => {
    forwarded.push(JSON.parse(JSON.stringify(value)));
    return { method: value.request.method, result: { syntheticExecutor: true, method: value.request.method } };
  },
});
const registered = [...handlers.keys()];
const results = [];
const temp = fs.mkdtempSync('/tmp/reign-ipc-');
const socketPath = path.join(temp, 'test.sock');
const clientId = 'isolated-test-client';
const ownerId = 'synthetic-app-owner';

function frame(value) {
  const data = Buffer.from(JSON.stringify(value));
  const head = Buffer.alloc(4);
  head.writeUInt32LE(data.length);
  return Buffer.concat([head, data]);
}
function receiver(onMessage) {
  let buffer = Buffer.alloc(0);
  return data => {
    buffer = Buffer.concat([buffer, data]);
    while (buffer.length >= 4) {
      const size = buffer.readUInt32LE(0);
      assert(size > 0 && size < 1024 * 1024);
      if (buffer.length < size + 4) break;
      const message = JSON.parse(buffer.subarray(4, size + 4));
      buffer = buffer.subarray(size + 4);
      onMessage(message);
    }
  };
}
const server = net.createServer(socket => {
  socket.on('data', receiver(async request => {
    const base = { type: 'response', requestId: request.requestId };
    if (request.method === 'initialize') {
      socket.write(frame({ ...base, resultType: 'success', result: { clientId } }));
      return;
    }
    const handler = handlers.get(request.method);
    if (!handler || !await handler.canHandle(request.params, request)) {
      socket.write(frame({ ...base, resultType: 'error', error: 'no-client-found' }));
      return;
    }
    try {
      socket.write(frame({ ...base, resultType: 'success', handledByClientId: ownerId,
        result: await handler.handle(request) }));
    } catch (error) {
      socket.write(frame({ ...base, resultType: 'error', error: error.message }));
    }
  }));
});

(async () => {
  let client;
  try {
    await new Promise(resolve => server.listen(socketPath, resolve));
    fs.chmodSync(socketPath, 0o600);
    client = net.createConnection(socketPath);
    await new Promise((resolve, reject) => { client.once('connect', resolve); client.once('error', reject); });
    const pending = new Map();
    client.on('data', receiver(message => {
      const p = pending.get(message.requestId);
      if (p) { pending.delete(message.requestId); clearTimeout(p.timer); p.resolve(message); }
    }));
    function request(method, params, hostId) {
      const requestId = randomUUID();
      const result = new Promise((resolve, reject) => {
        const timer = setTimeout(() => { pending.delete(requestId); reject(Error('fixture timeout')); }, 2000);
        pending.set(requestId, { resolve, timer });
      });
      const data = frame({ type: 'request', requestId, method, params, hostId, sourceClientId: clientId });
      // Fragment even the frame header to exercise stream reassembly.
      client.write(data.subarray(0, 2));
      client.write(data.subarray(2, 7));
      client.write(data.subarray(7));
      return result;
    }
    function check(name, condition) { assert(condition, name); results.push({ name, passed: true }); }
    check('isolated socket initialized', (await request('initialize', { clientType: 'reignhub-validation' })).result.clientId === clientId);
    check('installed discovery handler accepts held thread', (await request('thread-owner-discovery', { hostId: 'local', conversationId: fakeThread })).resultType === 'success');
    check('installed discovery handler rejects unknown thread', (await request('thread-owner-discovery', { hostId: 'local', conversationId: 'unknown' })).error === 'no-client-found');
    check('installed discovery handler rejects wrong host', (await request('thread-owner-discovery', { hostId: 'other', conversationId: fakeThread })).error === 'no-client-found');
    for (const method of ['thread-follower-start-turn', 'thread-follower-interrupt-turn',
      'thread-follower-command-approval-decision', 'thread-follower-load-complete-history',
      'thread-follower-set-queued-follow-ups-state']) {
      const value = await request(method, { conversationId: fakeThread, validationToken: method });
      check(method + ' forwarded by installed handler', value.resultType === 'success' && forwarded.at(-1).request.method === method && forwarded.at(-1).request.params.validationToken === method);
    }
    for (const method of ['thread/list', 'thread/read', 'thread/search', 'thread/start', 'thread/turns/list']) {
      const before = forwarded.length;
      const value = await request(method, { conversationId: fakeThread });
      check(method + ' has no installed owner handler', value.error === 'no-client-found' && forwarded.length === before);
    }
    const report = { scope: 'Offline installed handler registration + isolated Unix socket. Executor is synthetic; no live App/model calls.',
      manifest: JSON.parse(fs.readFileSync(path.join(root, 'manifest.json'), 'utf8')),
      registeredMethods: registered, registeredCount: registered.length, results, passed: results.length, failed: 0,
      capabilitiesNotVerified: ['real App discovery', 'real message execution', 'real snapshot/patch contents', 'cross-window input', 'model execution', 'complete App workflow'] };
    fs.writeFileSync(path.join(root, 'results.json'), JSON.stringify(report, null, 2) + '\n');
    console.log(JSON.stringify(report, null, 2));
  } finally {
    dispose();
    client?.destroy();
    await new Promise(resolve => server.close(resolve));
    fs.rmSync(temp, { recursive: true, force: true });
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
