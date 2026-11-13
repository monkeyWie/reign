import { DurableObject } from "cloudflare:workers";

interface Env {
  ROOMS: DurableObjectNamespace<RelayRoom>;
  AGENTHUB_RELAY_TOKEN: string;
  AGENTHUB_ALLOWED_ORIGINS?: string;
  ASSETS?: Fetcher;
}
interface Attachment {
  role: "host" | "client";
  peer: string;
  generation: string;
  closed: boolean;
}
interface Frame { peer?: string; kind: string; body?: string }
const MAX_FRAME_BYTES = 2 << 20;
const MAX_CLIENTS = 16;
const kinds = new Set(["hello", "signal", "chunk", "data"]);
const encoder = new TextEncoder();
function id(): string { return crypto.randomUUID().replaceAll("-", ""); }
function reject(message: string, status: number): Response {
  return new Response(message, { status, headers: { "Cache-Control": "no-store" } });
}
async function authorized(request: Request, token: string): Promise<boolean> {
  if (!token || token.length < 32) return false;
  let header = request.headers.get("Authorization") ?? "";
  // Browsers cannot set Authorization on a WebSocket upgrade. Keep credentials
  // out of URLs; only the non-secret protocol is echoed in the response.
  if (!header) {
    const protocols = (request.headers.get("Sec-WebSocket-Protocol") ?? "").split(",").map(p => p.trim());
    const auth = protocols.find(p => p.startsWith("auth."));
    if (protocols.includes("agenthub.v1") && auth) {
      try { header = `Bearer ${new TextDecoder().decode(Uint8Array.from(atob(auth.slice(5).replaceAll("-", "+").replaceAll("_", "/")), c => c.charCodeAt(0)))}`; }
      catch { return false; }
    }
  }
  if (header.length > 4096) return false;
  const [expected, actual] = await Promise.all([
    crypto.subtle.digest("SHA-256", encoder.encode(`Bearer ${token}`)),
    crypto.subtle.digest("SHA-256", encoder.encode(header)),
  ]);
  const a = new Uint8Array(expected), b = new Uint8Array(actual);
  let difference = 0;
  for (let i = 0; i < a.length; i++) difference |= a[i] ^ b[i];
  return difference === 0;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname === "/healthz" && request.method === "GET") {
      return new Response("ok", { headers: { "Cache-Control": "no-store" } });
    }
    if (url.pathname.startsWith("/v1/") || url.pathname.startsWith("/auth/")) return reject("not implemented", 404);
    if (url.pathname !== "/relay") return env.ASSETS ? env.ASSETS.fetch(request) : reject("not found", 404);
    // Authenticate before instantiating a Durable Object.
    if (!(await authorized(request, env.AGENTHUB_RELAY_TOKEN))) return reject("unauthorized", 401);
    const origin = request.headers.get("Origin");
    const allowed = (env.AGENTHUB_ALLOWED_ORIGINS ?? "").split(",").map(o => o.trim()).filter(Boolean);
    if (origin && origin !== url.origin && !allowed.includes(origin)) return reject("origin not allowed", 403);
    if (origin && url.searchParams.get("role") !== "client") return reject("browser host not allowed", 403);
    if (request.method !== "GET" || request.headers.get("Upgrade")?.toLowerCase() !== "websocket") {
      return reject("WebSocket upgrade required", 426);
    }
    const hostID = url.searchParams.get("id") ?? "";
    const role = url.searchParams.get("role");
    if (!/^[A-Za-z0-9_-]{16,128}$/.test(hostID) || (role !== "host" && role !== "client")) {
      return reject("invalid route", 400);
    }
    const room = env.ROOMS.get(env.ROOMS.idFromName(hostID));
    return room.fetch(request);
  },
} satisfies ExportedHandler<Env>;

// All state needed for routing lives in the hibernating sockets' attachments.
// There are no timers, outgoing connections, message logs, KV or SQL writes.
export class RelayRoom extends DurableObject<Env> {
  private metadata(ws: WebSocket): Attachment | null {
    return ws.deserializeAttachment() as Attachment | null;
  }
  private live(role: "host" | "client"): WebSocket[] {
    return this.ctx.getWebSockets(role).filter((ws) => {
      const state = this.metadata(ws);
      return state && !state.closed && ws.readyState === WebSocket.OPEN;
    });
  }
  private host(generation?: string): WebSocket | undefined {
    return this.live("host").find((ws) => !generation || this.metadata(ws)?.generation === generation);
  }
  private send(ws: WebSocket, frame: Frame): boolean {
    try { ws.send(JSON.stringify(frame)); return true; }
    catch { this.disconnect(ws, 1011, "peer unavailable"); return false; }
  }
  private disconnect(ws: WebSocket, code: number, reason: string): void {
    const state = this.metadata(ws);
    if (state && !state.closed) {
      state.closed = true;
      ws.serializeAttachment(state);
      if (state.role === "host") {
        for (const client of this.live("client")) {
          if (this.metadata(client)?.generation === state.generation) {
            this.disconnect(client, 1013, "host disconnected");
          }
        }
      } else {
        const host = this.host(state.generation);
        if (host) this.send(host, { peer: state.peer, kind: "bye" });
      }
    }
    try { ws.close(code, reason); } catch { /* Already closed by the runtime. */ }
  }
  async fetch(request: Request): Promise<Response> {
    const role = new URL(request.url).searchParams.get("role") as "host" | "client";
    const host = this.host();
    if (role === "host" && host) return reject("host already connected", 409);
    if (role === "client" && (!host || this.live("client").length >= MAX_CLIENTS)) {
      return reject("host unavailable or full", 503);
    }
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);
    const peer = id();
    const generation = role === "host" ? peer : this.metadata(host!)!.generation;
    server.serializeAttachment({ role, peer, generation, closed: false } satisfies Attachment);
    this.ctx.acceptWebSocket(server, [role]);
    server.send(JSON.stringify({ peer, kind: "welcome" } satisfies Frame));
    const protocols = (request.headers.get("Sec-WebSocket-Protocol") ?? "").split(",").map(p => p.trim());
    return new Response(null, { status: 101, webSocket: client,
      headers: protocols.includes("agenthub.v1") ? {"Sec-WebSocket-Protocol":"agenthub.v1"} : {} });
  }
  webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): void {
    const state = this.metadata(ws);
    if (!state || state.closed) return;
    if (typeof message !== "string") { this.disconnect(ws, 1003, "text frames required"); return; }
    if (message.length > MAX_FRAME_BYTES || encoder.encode(message).byteLength > MAX_FRAME_BYTES) {
      this.disconnect(ws, 1009, "frame too large"); return;
    }
    let frame: Frame;
    try { frame = JSON.parse(message) as Frame; }
    catch { this.disconnect(ws, 1008, "invalid JSON"); return; }
    if (!frame || typeof frame !== "object" || !kinds.has(frame.kind) || typeof frame.body !== "string") {
      this.disconnect(ws, 1008, "invalid frame"); return;
    }
    if (state.role === "host") {
      if (this.host(state.generation) !== ws) { this.disconnect(ws, 1008, "stale host"); return; }
      const target = this.live("client").find((client) => {
        const meta = this.metadata(client)!;
        return meta.generation === state.generation && meta.peer === frame.peer;
      });
      if (target) this.send(target, { peer: frame.peer, kind: frame.kind, body: frame.body });
    } else {
      const target = this.host(state.generation);
      if (!target) { this.disconnect(ws, 1013, "host unavailable"); return; }
      // Never trust a client-supplied peer field.
      this.send(target, { peer: state.peer, kind: frame.kind, body: frame.body });
    }
  }
  webSocketClose(ws: WebSocket, _code: number, _reason: string, _wasClean: boolean): void {
    this.disconnect(ws, 1000, "closed");
  }
  webSocketError(ws: WebSocket): void { this.disconnect(ws, 1011, "socket error"); }
}
