import {
  APIError,
  routes,
  type Device,
  type Identity,
  type Pairing,
  type ServiceConfig,
} from "@reignhub/protocol";
import { keypair, envelope, hex, random } from "./crypto";
import { progressStep, type ProgressObserver } from "./progress";
import { browserDeviceName } from "./browser-identity";
import { ReignHubCache, type SavedDevice } from "./cache";
export class Service {
  constructor(
    public origin: string,
    public cache: ReignHubCache,
    public observe?: ProgressObserver,
  ) {
    const u = new URL(origin);
    if (
      !["http:", "https:"].includes(u.protocol) ||
      u.username ||
      u.password ||
      u.pathname !== "/"
    )
      throw Error("请输入服务根地址");
    this.origin = u.origin;
  }
  async post<T>(
    path: string,
    input: unknown = {},
    token?: string,
    signal?: AbortSignal,
  ): Promise<T> {
    const r = await fetch(this.origin + path, {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        ...(token ? { Authorization: "Bearer " + token } : {}),
      },
      body: JSON.stringify(input),
      signal: signal
        ? AbortSignal.any([signal, AbortSignal.timeout(15000)])
        : AbortSignal.timeout(15000),
    });
    const data = (await r.json()) as { code: string; message: string };
    if (!r.ok)
      throw new APIError(
        data.code || "HTTP_ERROR",
        data.message || "请求失败",
        r.status,
      );
    return data as T;
  }
  async config() {
    this.observe?.(
      progressStep("config", "service", "获取连接配置", this.origin),
    );
    const config = await this.post<ServiceConfig>(routes.config);
    this.observe?.(
      progressStep(
        "config",
        "service",
        "连接配置已获取",
        this.origin,
        "success",
      ),
    );
    return config;
  }
  private identityTask?: Promise<Identity>;
  identity(): Promise<Identity> {
    return (this.identityTask ||= this.loadIdentity().catch((error) => {
      this.identityTask = undefined;
      throw error;
    }));
  }
  private async loadIdentity(): Promise<Identity> {
    this.observe?.(
      progressStep("identity", "service", "验证本机身份", this.origin),
    );
    let i = await this.cache.identities.get(this.origin);
    const name = await browserDeviceName();
    if (!i) {
      const pair = keypair();
      const r = await this.post<{
        id: string;
        connectionId: string;
        token: string;
      }>(routes.register, { name, public: pair.public, isHost: false });
      i = { ...r, ...pair, name, service: this.origin };
      await this.cache.identities.put(i);
    } else if (i.name !== name) {
      // Rename the same device; keep keys, token and existing authorizations.
      try {
        await this.post(routes.presence, { name }, i.token);
        i = { ...i, name };
        await this.cache.identities.put(i);
      } catch {
        /* A name refresh must not prevent an existing pairing from connecting. */
      }
    }
    this.observe?.(
      progressStep(
        "identity",
        "service",
        "本机身份已验证",
        this.origin,
        "success",
      ),
    );
    return i;
  }
  async resolve(code: string) {
    const i = await this.identity();
    this.observe?.(
      progressStep("resolve", "service", "查找设备并检查在线状态", code),
    );
    const result = await this.post<{ device: Device; online: boolean }>(
      routes.resolve,
      { connectionId: code },
      i.token,
    );
    this.observe?.(
      progressStep(
        "resolve",
        "service",
        result.online ? "被控设备在线" : "被控设备离线",
        result.device.name,
        result.online ? "success" : "warning",
      ),
    );
    return result;
  }
  async pair(
    code: string,
    password = "",
    signal?: AbortSignal,
    onWaiting?: () => void,
  ) {
    const i = await this.identity();
    const { device: d, online } = await this.resolve(code);
    if (!online)
      throw Error("被控设备离线，请确认电脑上的 ReignHub 正在运行并已连接服务");
    const previous = await this.cache.devices.get([
      this.origin,
      d.id,
    ] as unknown as string);
    if (previous && previous.public !== d.public)
      throw Error("设备身份已改变，请删除旧配对后重试");
    if (
      previous &&
      (!previous.expires || previous.expires > Date.now() / 1000)
    ) {
      this.observe?.(
        progressStep(
          "approval",
          "approval",
          "复用已有设备授权",
          "无需再次确认",
          "success",
        ),
      );
      return { ...previous, name: d.name };
    }
    const id = hex(random(16));
    const request = "reignhub/pair/request/" + id;
    const packet = await envelope(i.private, d.public, request, {
      password,
      remember: true,
    });
    await this.post(
      routes.request,
      { id, host: d.id, envelope: packet },
      i.token,
      signal,
    );
    this.observe?.(
      progressStep(
        "approval",
        "approval",
        password ? "等待电脑验证密码" : "等待被控电脑确认授权",
        d.name,
      ),
    );
    onWaiting?.();
    const deadline = Date.now() + 125000;
    while (Date.now() < deadline) {
      signal?.throwIfAborted();
      const p = await this.post<Pairing>(
        routes.result,
        { id },
        i.token,
        signal,
      );
      if (p.state === "denied") throw Error("被控设备拒绝授权或密码不正确");
      if (p.state === "approved") {
        const reply = (await envelope(
          i.private,
          d.public,
          "reignhub/pair/reply/" + id,
          undefined,
          p.reply,
        )) as { key: string; host: string; public: string; expires: number };
        if (reply.host !== d.id || reply.public !== d.public)
          throw Error("无效主机授权");
        const saved: SavedDevice = {
          ...d,
          key: reply.key,
          expires: reply.expires || 0,
          service: this.origin,
          identityId: i.id,
        };
        this.observe?.(
          progressStep(
            "approval",
            "approval",
            "设备授权已通过",
            d.name,
            "success",
          ),
        );
        await this.cache.devices.put(saved);
        return saved;
      }
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(resolve, 1000);
        signal?.addEventListener(
          "abort",
          () => {
            clearTimeout(timer);
            reject(signal.reason);
          },
          { once: true },
        );
      });
    }
    throw Error("等待被控设备授权超时");
  }
}
