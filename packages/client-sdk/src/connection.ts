import {
  routes,
  SOCKET_PROTOCOL,
  type Frame,
  type Identity,
  type RPCRequest,
  type ServiceConfig,
} from "@reignhub/protocol";
import {
  Cipher,
  b64,
  decode,
  hex,
  random,
  sum,
  un64,
  url64,
  utf8,
} from "./crypto";
import {
  CONNECTION_STAGE_TIMEOUT,
  progressStep,
  recordProgress,
  type ConnectionProgress,
  type ProgressObserver,
} from "./progress";
import type { SavedDevice } from "./cache";
interface Reply {
  id: string;
  result: unknown;
  error?: string;
  revision?: string;
  notModified?: boolean;
}
interface Pending {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
  timer?: ReturnType<typeof setTimeout>;
  cleanup?: () => void;
  key: string;
  cached?: { revision: string; value: unknown };
}
export class Connection extends EventTarget {
  private signal!: WebSocket;
  private opening = new Set<WebSocket>();
  private relay?: WebSocket;
  private pc?: RTCPeerConnection;
  private dc?: RTCDataChannel;
  private peer = "";
  private nonce = hex(random());
  private base!: Cipher;
  private cipher?: Cipher;
  private pending = new Map<string, Pending>();
  private snapshots = new Map<string, { revision: string; value: unknown }>();
  private chunks = new Map<
    string,
    { parts: (Uint8Array | undefined)[]; size: number; updated: number }
  >();
  private candidates: RTCIceCandidateInit[] = [];
  private chain = Promise.resolve();
  private sendChain = Promise.resolve();
  private tick?: ReturnType<typeof setInterval>;
  private closed = false;
  private established = false;
  private handshook!: () => void;
  private failHandshake!: (e: Error) => void;
  private heartbeat = Date.now();
  private pulses = 0;
  private preferDirectUntil = 0;
  progress: ConnectionProgress[] = [];
  path = "Connecting";
  allowRelay = false;
  constructor(
    public saved: SavedDevice,
    private identity: Identity,
    private config: ServiceConfig,
    private observe?: ProgressObserver,
  ) {
    super();
  }
  private report(
    id: string,
    stage: string,
    message: string,
    detail = "",
    state: ConnectionProgress["state"] = "running",
  ) {
    const step = progressStep(id, stage, message, detail, state);
    const next = recordProgress(this.progress, step);
    if (next === this.progress) return;
    this.progress = next;
    this.observe?.(step);
    this.dispatchEvent(new Event("progress"));
  }
  private update(path: string) {
    if (this.path !== path) {
      this.path = path;
      this.dispatchEvent(new Event("change"));
    }
  }
  private endpoint(path: string) {
    const u = new URL(path, this.saved.service);
    u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
    return u.origin + u.pathname;
  }
  private async socket(path: string, peer?: string) {
    if (this.closed) throw Error("连接已取消");
    const u = new URL(this.saved.service);
    u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
    u.pathname = path;
    u.searchParams.set("id", this.saved.id);
    u.searchParams.set("role", "client");
    if (peer) u.searchParams.set("peer", peer);
    const ws = new WebSocket(u, [
      SOCKET_PROTOCOL,
      "auth." + url64(utf8(this.identity.token)),
    ]);
    this.opening.add(ws);
    const welcome = await new Promise<Frame>((resolve, reject) => {
      const timeout = setTimeout(() => {
        ws.close();
        reject(
          Error(
            `${path === routes.relay ? "备用中继通道" : "信令连接"}超时（2 分钟）`,
          ),
        );
      }, CONNECTION_STAGE_TIMEOUT);
      ws.onerror = () => {
        clearTimeout(timeout);
        ws.close();
        reject(Error("连接服务失败，请检查服务地址和网络"));
      };
      ws.onmessage = (e) => {
        clearTimeout(timeout);
        try {
          const f = JSON.parse(e.data) as Frame;
          if (f.kind !== "welcome" || f.protocol !== 1) throw Error();
          resolve(f);
        } catch {
          ws.close();
          reject(Error("不支持的服务协议"));
        }
      };
      ws.onclose = () => {
        clearTimeout(timeout);
        reject(Error("服务拒绝连接，请检查配对和账号权益"));
      };
    }).finally(() => this.opening.delete(ws));
    if (this.closed) {
      ws.close();
      throw Error("连接已取消");
    }
    return { ws, welcome };
  }
  async connect() {
    try {
      this.report(
        "signal",
        "signal",
        "建立信令连接",
        this.endpoint(routes.signal),
      );
      const { ws, welcome } = await this.socket(routes.signal);
      this.report(
        "signal",
        "signal",
        "信令连接已建立",
        this.endpoint(routes.signal),
        "success",
      );
      this.signal = ws;
      this.peer = welcome.peer!;
      this.allowRelay = !!welcome.allowRelay;
      this.base = await Cipher.create(
        await sum("reign/p2p/peer/" + this.peer + "/" + this.saved.key),
        1,
        true,
      );
      ws.onmessage = (e) => {
        this.heartbeat = Date.now();
        if (e.data === "reign:pong") return;
        this.chain = this.chain
          .then(() => this.receive(JSON.parse(e.data) as Frame))
          .catch(() => this.close("连接数据校验失败"));
      };
      ws.onclose = () => this.close("信令连接已断开");
      ws.onerror = () => this.close("信令连接异常");
      this.tick = setInterval(() => {
        if (Date.now() - this.heartbeat > 85000) {
          this.close("连接心跳超时");
          return;
        }
        if (this.signal.readyState === WebSocket.OPEN) {
          this.signal.send("reign:ping");
          if (++this.pulses % 3 === 0)
            this.signal.send(JSON.stringify({ kind: "renew" }));
        }
        if (this.relay?.readyState === WebSocket.OPEN)
          this.relay.send("reign:ping");
      }, 25000);
      if (this.allowRelay) {
        this.report(
          "relay-ready",
          "relay",
          "准备备用中继通道",
          this.endpoint(routes.relay),
        );
        try {
          const data = await this.socket(routes.relay, this.peer);
          this.relay = data.ws;
          this.report(
            "relay-ready",
            "relay",
            "备用中继通道已就绪",
            this.endpoint(routes.relay) + " · 优先直连",
            "success",
          );
          this.relay.onclose = () => {
            this.relay = undefined;
            if (!this.dc || this.dc.readyState !== "open") {
              if (this.established) this.close("数据连接已断开");
              else this.update("Disconnected");
            }
          };
          this.relay.onmessage = (e) => {
            if (e.data !== "reign:pong") {
              try {
                const f = JSON.parse(e.data) as Frame;
                if (f.kind === "error")
                  this.dispatchEvent(
                    new CustomEvent("error", { detail: f.error }),
                  );
              } catch {}
            }
          };
        } catch (error) {
          if (this.closed) throw error;
          this.report(
            "relay-ready",
            "relay",
            "备用中继暂不可用，继续尝试 P2P 直连",
            error instanceof Error ? error.message : String(error),
            "warning",
          );
        }
      }
      const ready = new Promise<void>((resolve, reject) => {
        this.handshook = resolve;
        this.failHandshake = reject;
      });
      const timeout = setTimeout(
        () => this.failHandshake(Error("配对握手超时")),
        CONNECTION_STAGE_TIMEOUT,
      );
      this.report("handshake", "handshake", "与被控设备建立加密握手");
      await this.sendFrame("hello", { clientNonce: this.nonce });
      try {
        await ready;
        this.report(
          "handshake",
          "handshake",
          "端到端加密握手完成",
          "已验证设备配对密钥",
          "success",
        );
      } finally {
        clearTimeout(timeout);
      }
      this.update("Connecting · direct first");
      if (typeof RTCPeerConnection !== "undefined") {
        try {
          await this.offer();
        } catch (e) {
          this.report(
            "ice",
            "udp",
            "WebRTC 探测未能启动",
            e instanceof Error ? e.message : String(e),
            "warning",
          );
          if (!this.relay) throw e;
        }
      } else {
        this.report(
          "ice",
          "udp",
          "当前浏览器不提供 WebRTC",
          "使用可用的服务中继",
          "warning",
        );
      }
      this.report("direct", "direct", "正在验证直连或中继路径");
      await this.waitPath();
      this.report("verify", "verify", "正在验证设备响应");
      await this.call({ method: "providers" }, CONNECTION_STAGE_TIMEOUT);
      this.established = true;
      this.report(
        "connected",
        "connected",
        "设备连接成功",
        this.path,
        "success",
      );
      return this;
    } catch (e) {
      this.close();
      throw e;
    }
  }
  private aad(kind: string, out = true) {
    return "reign/p2p/" + this.peer + "/" + (out ? "c2h" : "h2c") + "/" + kind;
  }
  private async sendFrame(kind: string, value: unknown) {
    const cipher = kind === "hello" ? this.base : this.cipher;
    if (!cipher) throw Error("握手未完成");
    const body = b64(
      await cipher.seal(this.aad(kind), utf8(JSON.stringify(value))),
    );
    this.signal.send(JSON.stringify({ peer: this.peer, kind, body }));
  }
  private async receive(f: Frame) {
    if (f.kind === "error") throw Error(f.error);
    if (f.kind === "lease") return;
    if (!f.body) return;
    const cipher = f.kind === "hello" ? this.base : this.cipher;
    if (!cipher) return;
    const plain = await cipher.open(this.aad(f.kind, false), un64(f.body));
    if (f.kind === "hello") {
      if (this.cipher) return;
      const h = JSON.parse(decode(plain)) as {
        clientNonce: string;
        hostNonce: string;
      };
      if (h.clientNonce !== this.nonce || !/^[a-f0-9]{64}$/.test(h.hostNonce))
        throw Error("无效配对握手");
      this.cipher = await Cipher.create(
        await sum(
          "reign/p2p/session/" +
            this.saved.key +
            "/" +
            this.peer +
            "/" +
            h.clientNonce +
            "/" +
            h.hostNonce,
        ),
      );
      this.handshook();
      return;
    }
    if (f.kind === "signal") {
      const s = JSON.parse(decode(plain)) as RTCSessionDescriptionInit & {
        candidate?: RTCIceCandidateInit;
      };
      if ((s.type as string) === "candidate") {
        if (s.candidate) {
          if (this.pc?.remoteDescription)
            await this.pc.addIceCandidate(s.candidate);
          else if (this.candidates.length < 256)
            this.candidates.push(s.candidate);
        }
        return;
      }
      if (s.type === "answer" && this.pc) {
        await this.pc.setRemoteDescription(s);
        for (const c of this.candidates) await this.pc.addIceCandidate(c);
        this.candidates = [];
      }
      return;
    }
    if (f.kind === "chunk") this.assemble(plain);
    else if (f.kind === "data") this.response(plain);
  }
  private async offer() {
    this.preferDirectUntil = Date.now() + 1500;
    for (const url of this.config.publicStun)
      this.report(
        "stun:" + url,
        "udp",
        "公共 STUN 节点",
        url + " · 并行探测",
        "info",
      );
    for (const url of this.config.selfStun)
      this.report(
        "stun:" + url,
        "udp",
        "自建 STUN 节点",
        url + " · 并行探测",
        "info",
      );
    this.report(
      "tcp",
      "tcp",
      "浏览器使用 WebRTC 直连",
      "原生 TCP 打洞由桌面端和移动端支持",
      "info",
    );
    this.report("ice", "udp", "收集 UDP / IPv6 直连候选地址");
    this.pc = new RTCPeerConnection({
      iceServers: [
        ...new Set([...this.config.publicStun, ...this.config.selfStun]),
      ]
        .filter((x) => x.startsWith("stun:"))
        .map((url) => ({ urls: url })),
    });
    const pc = this.pc;
    pc.onicecandidate = (e) => {
      if (!e.candidate) {
        this.report(
          "ice",
          "udp",
          "UDP 候选地址收集结束",
          "正在检查可用直连路径",
          "success",
        );
        return;
      }
      this.report(
        "candidate:" +
          e.candidate.foundation +
          e.candidate.address +
          e.candidate.port,
        "udp",
        "发现 UDP 直连候选",
        `${e.candidate.type} · ${e.candidate.address || "本地地址"}:${e.candidate.port} · ${e.candidate.protocol}`,
        "info",
      );
      if (e.candidate)
        void this.sendFrame("signal", {
          type: "candidate",
          candidate: e.candidate.toJSON(),
        }).catch(() => {});
    };
    pc.onicegatheringstatechange = () => {
      if (pc.iceGatheringState === "complete")
        void pc
          .getStats()
          .then((stats) =>
            stats.forEach((stat) => {
              if (stat.type === "local-candidate" && stat.url && !this.closed)
                this.report(
                  "stun:" + stat.url,
                  "udp",
                  "STUN 节点已返回公网映射",
                  `${stat.url} → ${stat.address || stat.ip || "公网地址"}:${stat.port}`,
                  "success",
                );
            }),
          )
          .catch(() => {});
    };
    pc.onicecandidateerror = (e) =>
      this.report(
        "stun-error:" + e.url,
        "udp",
        "STUN 节点探测未成功",
        `${e.url} · ${e.errorCode} ${e.errorText}`,
        "warning",
      );
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === "connecting")
        this.report("ice-check", "udp", "正在验证 WebRTC 直连路径");
      if (pc.connectionState === "failed")
        this.report(
          "ice-check",
          "udp",
          "WebRTC 直连暂未建立",
          "使用可用的服务中继",
          "warning",
        );
      if (["failed", "disconnected", "closed"].includes(pc.connectionState)) {
        if (this.relay?.readyState === WebSocket.OPEN)
          this.update("Relay · encrypted");
        else if (this.established) this.close("直连已断开，且没有可用中继");
        else this.update("Disconnected");
      }
    };
    this.dc = pc.createDataChannel("reign.p2p", { ordered: true });
    this.dc.binaryType = "arraybuffer";
    this.dc.onopen = () => {
      this.update("P2P · WebRTC");
      this.report(
        "path:webrtc",
        "direct",
        "已切换为 WebRTC 直连",
        "端到端加密数据通道已建立",
        "success",
      );
      void pc
        .getStats()
        .then((stats) => {
          stats.forEach((stat) => {
            if (stat.type === "transport" && stat.selectedCandidatePairId) {
              const pair = stats.get(stat.selectedCandidatePairId),
                local = stats.get(pair?.localCandidateId),
                remote = stats.get(pair?.remoteCandidateId);
              if (
                local &&
                remote &&
                !this.closed &&
                this.dc?.readyState === "open"
              )
                this.report(
                  "path:webrtc",
                  "direct",
                  "已切换为 WebRTC 直连",
                  `${local.protocol} · ${local.address || local.ip || local.candidateType}:${local.port} → ${remote.address || remote.ip || remote.candidateType}:${remote.port}`,
                  "success",
                );
            }
          });
        })
        .catch(() => {});
    };
    this.dc.onclose = () =>
      this.update(this.relay ? "Relay · encrypted" : "Disconnected");
    this.dc.onmessage = (e) => {
      this.chain = this.chain
        .then(async () => {
          if (e.data instanceof ArrayBuffer && this.cipher)
            this.assemble(
              await this.cipher.open(
                this.aad("chunk", false),
                new Uint8Array(e.data),
              ),
            );
        })
        .catch(() => this.close("加密数据异常"));
    };
    await pc.setLocalDescription(await pc.createOffer());
    await this.sendFrame("signal", pc.localDescription!.toJSON());
  }
  private assemble(plain: Uint8Array) {
    const c = JSON.parse(decode(plain)) as {
      id: string;
      i: number;
      n: number;
      b: string;
    };
    if (
      !Number.isInteger(c.n) ||
      c.n < 1 ||
      c.n > 1400 ||
      !Number.isInteger(c.i) ||
      c.i < 0 ||
      c.i >= c.n
    )
      return;
    const part = un64(c.b);
    if (part.length > 12000) return;
    for (const [k, v] of this.chunks)
      if (Date.now() - v.updated > CONNECTION_STAGE_TIMEOUT)
        this.chunks.delete(k);
    let a = this.chunks.get(c.id);
    if (!a) {
      if (this.chunks.size >= 8) return;
      a = { parts: Array(c.n), size: 0, updated: Date.now() };
      this.chunks.set(c.id, a);
    }
    if (a.parts.length !== c.n || a.parts[c.i]) return;
    a.updated = Date.now();
    a.parts[c.i] = part;
    a.size += part.length;
    if (a.size > 16 * 1024 * 1024) {
      this.chunks.delete(c.id);
      return;
    }
    if (a.parts.filter(Boolean).length !== c.n) return;
    const full = new Uint8Array(a.size);
    let at = 0;
    for (const p of a.parts) {
      full.set(p!, at);
      at += p!.length;
    }
    this.chunks.delete(c.id);
    this.response(full);
  }
  private response(raw: Uint8Array) {
    const r = JSON.parse(decode(raw)) as Reply;
    const p = this.pending.get(r.id);
    if (!p) return;
    clearTimeout(p.timer);
    p.cleanup?.();
    this.pending.delete(r.id);
    if (r.error) {
      p.reject(Error(r.error));
      return;
    }
    if (r.notModified) {
      if (!p.cached || p.cached.revision !== r.revision) {
        p.reject(Error("缓存版本不匹配"));
        return;
      }
      r.result = p.cached.value;
    } else if (p.key && r.revision) {
      if (this.snapshots.size >= 32)
        this.snapshots.delete(this.snapshots.keys().next().value!);
      this.snapshots.set(p.key, { revision: r.revision, value: r.result });
    }
    p.resolve(r.result);
  }
  private async waitPath() {
    const deadline = Date.now() + CONNECTION_STAGE_TIMEOUT;
    while (
      !this.closed &&
      this.dc?.readyState !== "open" &&
      (!this.relay || Date.now() < this.preferDirectUntil)
    ) {
      if (Date.now() > deadline)
        throw Error(
          this.allowRelay
            ? "直连失败，备用中继通道也未能建立，请查看连接详情"
            : "直连失败，当前服务未提供中继权限",
        );
      await new Promise((r) => setTimeout(r, 30));
    }
    if (this.closed) throw Error("连接已断开");
  }
  private async send(raw: Uint8Array) {
    if (raw.length > 1024 * 1024) throw Error("请求过大");
    await this.waitPath();
    if (this.closed || !this.cipher) throw Error("连接已断开");
    const id = hex(random(16));
    const n = Math.max(1, Math.ceil(raw.length / 12000));
    for (let i = 0; i < n; i++) {
      const part = utf8(
        JSON.stringify({
          id,
          i,
          n,
          b: b64(raw.slice(i * 12000, (i + 1) * 12000)),
        }),
      );
      const sealed = await this.cipher.seal(this.aad("chunk"), part);
      if (this.dc?.readyState === "open") {
        const until = Date.now() + 8000;
        while (this.dc.bufferedAmount > 512 * 1024) {
          if (Date.now() > until || this.closed)
            throw Error("直连发送阻塞，请刷新确认结果后再重试");
          await new Promise((r) => setTimeout(r, 10));
        }
        this.dc.send(sealed as unknown as ArrayBuffer);
      } else if (this.relay?.readyState === WebSocket.OPEN) {
        this.update("Relay · encrypted");
        this.report(
          "path:relay",
          "relay",
          "正在使用加密中继",
          "直连尚未建立，先通过服务中继；后台继续尝试直连",
          "success",
        );
        this.relay.send(
          JSON.stringify({ peer: this.peer, kind: "chunk", body: b64(sealed) }),
        );
      } else throw Error("发送中断，请刷新确认结果后再重试");
    }
  }
  async call<T = unknown>(
    request: RPCRequest,
    timeout?: number,
    signal?: AbortSignal,
  ): Promise<T> {
    if (this.closed) throw Error("连接已断开");
    signal?.throwIfAborted();
    const r: RPCRequest & { id: string } = {
      ...request,
      id: crypto.randomUUID(),
    };
    const readOnly = [
      "providers",
      "network",
      "sessions",
      "transcript",
      "approvals",
      "sync",
      "models",
    ].includes(r.method);
    const key = readOnly
      ? JSON.stringify({ ...r, id: undefined, revision: undefined })
      : "";
    const cached = this.snapshots.get(key);
    if (cached) r.revision = cached.revision;
    const result = new Promise<T>((resolve, reject) => {
      // Ordinary RPCs wait for the result, cancellation or connection loss.
      // Only setup verification passes an explicit deadline.
      const timer =
        timeout === undefined
          ? undefined
          : setTimeout(() => {
              this.pending.get(r.id)?.cleanup?.();
              this.pending.delete(r.id);
              reject(Error("请求超时，请刷新确认操作结果后再重试"));
            }, timeout);
      const abort = () => {
        clearTimeout(timer);
        this.pending.get(r.id)?.cleanup?.();
        this.pending.delete(r.id);
        reject(signal?.reason ?? new DOMException("请求已取消", "AbortError"));
      };
      signal?.addEventListener("abort", abort, { once: true });
      this.pending.set(r.id, {
        resolve: resolve as (v: unknown) => void,
        reject,
        timer,
        cleanup: () => signal?.removeEventListener("abort", abort),
        key,
        cached,
      });
    });
    this.sendChain = this.sendChain
      .catch(() => {})
      .then(() => {
        if (this.pending.has(r.id)) return this.send(utf8(JSON.stringify(r)));
      });
    void this.sendChain.catch((e) => {
      const p = this.pending.get(r.id);
      if (p) {
        clearTimeout(p.timer);
        p.cleanup?.();
        this.pending.delete(r.id);
        p.reject(e as Error);
      }
    });
    return result;
  }
  close(reason = "Disconnected") {
    if (this.closed) return;
    this.closed = true;
    if (reason !== "Disconnected")
      this.report(
        "disconnected",
        "connection",
        "连接已断开",
        reason,
        "warning",
      );
    clearInterval(this.tick);
    this.failHandshake?.(Error(reason));
    for (const socket of this.opening) socket.close();
    this.opening.clear();
    this.signal?.close();
    this.relay?.close();
    this.pc?.close();
    for (const p of this.pending.values()) {
      clearTimeout(p.timer);
      p.cleanup?.();
      p.reject(Error(reason));
    }
    this.pending.clear();
    this.update("Disconnected");
    this.dispatchEvent(new CustomEvent("disconnect", { detail: reason }));
  }
}
