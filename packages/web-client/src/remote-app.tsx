"use client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ConnectionPanel, type ConnectionAttempt } from "./connection-panel";
import { MessageContent } from "./message-content-view";
import { readableMessage } from "./message-content";
import {
  ArrowLeft,
  ArrowDown,
  Wrench,
  Wifi,
  WifiOff,
  ArrowUp,
  Check,
  ChevronDown,
  Copy,
  Folder,
  Globe,
  Link as LinkIcon,
  LoaderCircle,
  Monitor,
  MoreHorizontal,
  Plus,
  Search,
  Settings,
  Shield,
  Square,
  Sparkles,
  Terminal,
  Trash2,
  User,
  X,
} from "lucide-react";
import {
  Connection,
  ReignHubCache,
  Service,
  recordProgress,
  type ConnectionProgress,
  type SavedDevice,
} from "@reignhub/client-sdk";
import type { Item, Provider, Session } from "@reignhub/protocol";
import { Button, Dialog, ErrorNotice, copyText } from "./ui";
import { Attachments, upload } from "./attachments";
import {
  applyEvents,
  reconcileTranscript,
  type Batch,
  type Transcript,
} from "./transcript";
import {
  messages,
  isRunning,
  activeTurn,
  showThinking,
  activityLabel,
  navigationRows,
  uniqueSessions,
} from "./presentation";
const errorText = (e: unknown) => (e instanceof Error ? e.message : String(e));
interface Selected {
  device: string;
  provider: string;
  id: string;
}
function deviceKey(d: SavedDevice) {
  return d.service + "|" + d.id;
}
export function RemoteApp() {
  const [cloudDevices, setCloudDevices] = useState<
    { id: string; name: string; connectionId: string }[]
  >([]);
  const [cache] = useState(() => new ReignHubCache());
  const [service, setService] = useState(() => location.origin);
  const [devices, setDevices] = useState<SavedDevice[]>([]);
  const [connections, setConnections] = useState<Record<string, Connection>>(
    {},
  );
  const connectionRef = useRef(connections);
  connectionRef.current = connections;
  const [selectedDevice, setSelectedDevice] = useState("");
  const [providers, setProviders] = useState<Provider[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [selected, setSelected] = useState<Selected>();
  const [transcript, setTranscript] = useState<Transcript>();
  const transcriptScope = useRef("");
  const transcriptRef = useRef(transcript);
  transcriptRef.current = transcript;
  const [error, setError] = useState("");
  const [pairDialog, setPairDialog] = useState(false);
  const [settings, setSettings] = useState(false);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [pairing, setPairing] = useState(false);
  const [attempts, setAttempts] = useState<Record<string, ConnectionAttempt>>(
    {},
  );
  const [connectionPanel, setConnectionPanel] = useState("");
  const attemptResources = useRef(
    new Map<
      string,
      {
        cancelled: boolean;
        active: boolean;
        controller: AbortController;
        connection?: Connection;
      }
    >(),
  );
  const [search, setSearch] = useState("");
  const [view, setView] = useState<"devices" | "sessions" | "chat">("devices");
  const pairAbort = useRef<AbortController | undefined>(undefined);
  const forgotten = useRef(new Set<string>());
  const disposed = useRef(false);
  const [reconnecting, setReconnecting] = useState<Record<string, boolean>>({});
  const reconnectTimers = useRef(
    new Map<string, ReturnType<typeof setTimeout>>(),
  );
  const onError = useCallback((e: unknown) => setError(errorText(e)), []);
  const active = connections[selectedDevice];
  const current = devices.find((d) => deviceKey(d) === selectedDevice);
  useEffect(() => {
    disposed.current = false;
    const params = new URLSearchParams(location.search);
    if (params.get("device")) {
      setCode(params.get("device")!.replace(/[^0-9]/g, ""));
      setPairDialog(true);
    }
    void fetch("/api/account", { credentials: "include" })
      .then((r) => r.json())
      .then((r) => setCloudDevices(r.account?.devices || []))
      .catch(() => {});
    void cache.devices.toArray().then(setDevices).catch(onError);
    const onPop = () => setView("sessions");
    window.addEventListener("popstate", onPop);
    return () => {
      disposed.current = true;
      window.removeEventListener("popstate", onPop);
      pairAbort.current?.abort();
      for (const attempt of attemptResources.current.values()) {
        attempt.cancelled = true;
        attempt.controller.abort();
        attempt.connection?.close();
      }
      for (const t of reconnectTimers.current.values()) clearTimeout(t);
      for (const c of Object.values(connectionRef.current)) c.close();
    };
  }, []);

  function startAttempt(
    key: string,
    data: Omit<ConnectionAttempt, "active" | "steps" | "started">,
    seed?: ConnectionAttempt,
  ) {
    const previous = attemptResources.current.get(key);
    if (previous) {
      previous.cancelled = true;
      previous.controller.abort();
      previous.connection?.close();
    }
    const resource = {
      cancelled: false,
      active: true,
      controller: new AbortController(),
      connection: undefined as Connection | undefined,
    };
    attemptResources.current.set(key, resource);
    const value: ConnectionAttempt = {
      ...data,
      active: true,
      steps: seed?.steps || [],
      started: seed?.started || Date.now(),
    };
    setAttempts((all) => ({ ...all, [key]: value }));
    const observe = (step: ConnectionProgress) => {
      if (
        disposed.current ||
        resource.cancelled ||
        attemptResources.current.get(key) !== resource
      )
        return;
      setAttempts((all) => ({
        ...all,
        [key]: {
          ...(all[key] || value),
          steps: recordProgress(all[key]?.steps || value.steps, step),
          path: resource.connection?.path || all[key]?.path,
        },
      }));
    };
    const finish = (error?: unknown) => {
      if (
        disposed.current ||
        resource.cancelled ||
        attemptResources.current.get(key) !== resource
      )
        return;
      resource.active = false;
      setAttempts((all) => ({
        ...all,
        [key]: {
          ...(all[key] || value),
          active: false,
          finished: Date.now(),
          error: error ? errorText(error) : undefined,
          path: resource.connection?.path,
        },
      }));
    };
    return { resource, observe, finish };
  }
  function cancelAttempt(key: string) {
    const resource = attemptResources.current.get(key);
    if (resource) {
      resource.cancelled = true;
      resource.active = false;
      resource.controller.abort();
      resource.connection?.close();
    }
    const timer = reconnectTimers.current.get(key);
    if (timer) clearTimeout(timer);
    reconnectTimers.current.delete(key);
    forgotten.current.add(key);
    if (key === "pair") setPairing(false);
    setReconnecting((all) => ({ ...all, [key]: false }));
    setAttempts((all) => ({
      ...all,
      [key]: {
        ...all[key],
        active: false,
        path: "已取消",
        finished: Date.now(),
      },
    }));
  }
  async function connect(
    d: SavedDevice,
    automatic = false,
    force = false,
    seed?: ConnectionAttempt,
  ) {
    const key = deviceKey(d);
    if (disposed.current || (automatic && forgotten.current.has(key))) return;
    if (!automatic) {
      forgotten.current.delete(key);
      setConnectionPanel(key);
    }
    if (attemptResources.current.get(key)?.active) return;
    if (
      connectionRef.current[key] &&
      connectionRef.current[key].path !== "Disconnected" &&
      !force
    )
      return;
    const timer = reconnectTimers.current.get(key);
    if (timer) clearTimeout(timer);
    reconnectTimers.current.delete(key);
    const { resource, observe, finish } = startAttempt(
      key,
      { name: d.name, service: d.service, code: d.connectionId },
      seed,
    );
    setReconnecting((x) => ({ ...x, [key]: true }));
    try {
      const api = new Service(d.service, cache, observe);
      const identity = await api.identity();
      const config = await api.config();
      const resolved = await api.resolve(d.connectionId);
      if (resource.cancelled) return;
      if (!resolved.online)
        throw Error(
          "被控设备离线，请确认电脑上的 ReignHub 正在运行并已连接服务",
        );
      if (resolved.device.id !== d.id || resolved.device.public !== d.public)
        throw Error("设备身份已改变，请重新配对");
      const latest = { ...d, name: resolved.device.name };
      await cache.devices.put(latest);
      const c = new Connection(latest, identity, config, observe);
      resource.connection = c;
      c.addEventListener("change", () => {
        if (
          resource.cancelled ||
          attemptResources.current.get(key) !== resource
        )
          return;
        setConnections((x) => ({ ...x }));
        setAttempts((all) =>
          all[key] ? { ...all, [key]: { ...all[key], path: c.path } } : all,
        );
      });
      let established = false;
      c.addEventListener("disconnect", () => {
        if (
          !established ||
          disposed.current ||
          resource.cancelled ||
          attemptResources.current.get(key) !== resource ||
          forgotten.current.has(key)
        )
          return;
        setConnections((x) => ({ ...x }));
        if (!reconnectTimers.current.has(key)) {
          const timer = setTimeout(() => {
            reconnectTimers.current.delete(key);
            void connect(latest, true);
          }, 5000);
          reconnectTimers.current.set(key, timer);
        }
      });
      await c.connect();
      established = true;
      if (resource.cancelled) {
        c.close();
        return;
      }
      finish();
      setConnections((x) => ({ ...x, [key]: c }));
      setDevices(await cache.devices.toArray());
      if (!automatic) {
        setSelectedDevice(key);
        setView("sessions");
      }
    } catch (e) {
      finish(e);
      if (automatic && !resource.cancelled && !reconnectTimers.current.has(key))
        reconnectTimers.current.set(
          key,
          setTimeout(() => {
            reconnectTimers.current.delete(key);
            void connect(d, true);
          }, 10000),
        );
    } finally {
      if (attemptResources.current.get(key) === resource)
        setReconnecting((x) => ({ ...x, [key]: false }));
    }
  }
  async function remove(d: SavedDevice) {
    const key = deviceKey(d);
    forgotten.current.add(key);
    cancelAttempt(key);
    const timer = reconnectTimers.current.get(key);
    if (timer) clearTimeout(timer);
    reconnectTimers.current.delete(key);
    const c = connectionRef.current[key];
    if (c) {
      setConnections((x) => {
        const n = { ...x };
        delete n[key];
        return n;
      });
      c.close();
    }
    await cache.devices.delete([d.service, d.id] as unknown as string);
    setDevices(await cache.devices.toArray());
    if (selectedDevice === key) {
      setSelectedDevice("");
      setSelected(undefined);
      setView("devices");
    }
  }
  async function pair() {
    if (pairing) return;
    setPairing(true);
    setPairDialog(false);
    setConnectionPanel("pair");
    const started = Date.now();
    let steps: ConnectionProgress[] = [];
    const { resource, observe, finish } = startAttempt("pair", {
      name: "设备 " + code,
      service,
      code,
    });
    pairAbort.current = resource.controller;
    try {
      const api = new Service(service, cache, (step) => {
        steps = recordProgress(steps, step);
        observe(step);
      });
      const d = await api.pair(code, password, resource.controller.signal);
      if (resource.cancelled) return;
      setDevices(await cache.devices.toArray());
      finish();
      await connect(d, false, false, {
        name: d.name,
        service,
        code,
        started,
        steps,
        active: true,
      });
      setPassword("");
    } catch (e) {
      if (!resource.cancelled) finish(e);
    } finally {
      if (attemptResources.current.get("pair") === resource) setPairing(false);
    }
  }
  useEffect(() => {
    if (!active || active.path === "Disconnected") {
      // Keep the last catalogue visible while reconnecting.
      if (!selectedDevice) {
        setProviders([]);
        setSessions([]);
      }
      return;
    }
    let alive = true;
    const key = selectedDevice + "|sessions";
    void cache.snapshots.get(key).then((r) => {
      if (alive && r) setSessions(r.value as Session[]);
    });
    async function refresh() {
      try {
        const ps = await active.call<Provider[]>({ method: "providers" });
        let all: Session[] = [];
        for (const p of ps.filter(
          (p) => p.controllable && !p.id.includes("vscode"),
        )) {
          let cursor = "";
          let pages = 0;
          do {
            const page = await active.call<{
              sessions: Session[];
              nextCursor?: string;
            }>({ method: "sessions", provider: p.id, cursor });
            all.push(...page.sessions.map((s) => ({ ...s, providerId: p.id })));
            cursor = page.nextCursor || "";
          } while (cursor && ++pages < 100);
        }
        all = uniqueSessions(all);
        if (alive) {
          setProviders(ps);
          setSessions(all);
          await cache.putSnapshot(key, all);
        }
      } catch (e) {
        if (alive) onError(e);
      }
    }
    void refresh();
    const timer = setInterval(() => void refresh(), 10000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [active, selectedDevice]);
  useEffect(() => {
    if (!selected || !active) return;
    const scope = [selected.device, selected.provider, selected.id].join("|");
    if (transcriptScope.current !== scope) {
      transcriptRef.current = undefined;
      setTranscript(undefined);
    }
    transcriptScope.current = scope;
    let alive = true;
    let snapshot: Transcript | undefined;
    let epoch = "",
      cursor = 0;
    const key = [selected.device, selected.provider, selected.id].join("|");
    void cache.snapshots.get(key).then((r) => {
      if (alive && r && !snapshot && !transcriptRef.current) {
        transcriptRef.current = r.value as Transcript;
        setTranscript(r.value as Transcript);
      }
    });
    async function full() {
      const t = await active.call<Transcript>({
        method: "transcript",
        provider: selected!.provider,
        session: selected!.id,
      });
      if (!alive) return t;
      snapshot = reconcileTranscript(
        t,
        transcriptScope.current === key ? transcriptRef.current : undefined,
      );
      if (alive) {
        transcriptRef.current = snapshot;
        setTranscript(snapshot);
        void cache.putSnapshot(key, snapshot);
      }
      return t;
    }
    void (async () => {
      while (alive && active.path !== "Disconnected") {
        try {
          const baseline = await active.call<Batch>({
            method: "watch",
            provider: selected.provider,
            session: selected.id,
            epoch: "",
            after: 0,
          });
          epoch = baseline.epoch;
          cursor = baseline.cursor;
          await full();
          while (alive && active.path !== "Disconnected") {
            const batch = await active.call<Batch>({
              method: "watch",
              provider: selected.provider,
              session: selected.id,
              epoch,
              after: cursor,
            });
            if (!alive) return;
            if (
              batch.reset ||
              batch.events.some((e) => e.kind === "snapshot")
            ) {
              await full();
            } else if (snapshot && batch.events.length) {
              try {
                snapshot = reconcileTranscript(
                  applyEvents(snapshot, batch.events),
                  transcriptRef.current,
                );
              } catch {
                await full();
              }
              transcriptRef.current = snapshot;
              setTranscript(snapshot);
              void cache.putSnapshot(key, snapshot);
              if (
                batch.events.some(
                  (e) => e.kind === "turn" && !isRunning(e.status),
                )
              )
                await full();
            }
            epoch = batch.epoch;
            cursor = batch.cursor;
          }
        } catch (e) {
          if (alive) {
            try {
              await full();
            } catch {}
            // Retry transient watch errors without clearing the cached transcript.
            await new Promise((resolve) => setTimeout(resolve, 1500));
          }
        }
      }
    })();
    return () => {
      alive = false;
    };
  }, [active, selected?.device, selected?.provider, selected?.id]);
  function selectSession(s: Session) {
    setSelected({ device: selectedDevice, provider: s.providerId, id: s.id });
    setView("chat");
    if (matchMedia("(max-width: 999px)").matches)
      history.pushState({ reignChat: true }, "");
  }
  async function create() {
    if (!active) return;
    const p = providers.find((p) => p.controllable && p.capabilities.create);
    if (!p) {
      onError("此设备没有可以新建会话的 Agent，请先在被控端启用");
      return;
    }
    const cwd = prompt("项目工作目录", transcript?.session.cwd || "");
    if (!cwd) return;
    try {
      const s = await active.call<Session>({
        method: "create",
        provider: p.id,
        options: { cwd },
      });
      s.providerId = p.id;
      setSessions((x) => [s, ...x]);
      selectSession(s);
    } catch (e) {
      onError(e);
    }
  }
  const groups = useMemo(() => {
    const m = new Map<string, Session[]>();
    for (const s of sessions) {
      if (
        search &&
        !`${s.title} ${s.preview} ${s.cwd}`
          .toLowerCase()
          .includes(search.toLowerCase())
      )
        continue;
      const key = s.cwd || "其他会话";
      const list = m.get(key) || [];
      list.push(s);
      m.set(key, list);
    }
    return [...m.entries()].sort(
      (a, b) =>
        Math.max(...b[1].map((s) => s.updatedAt)) -
        Math.max(...a[1].map((s) => s.updatedAt)),
    );
  }, [sessions, search]);
  return (
    <div className={"remote-app view-" + view}>
      <aside className="device-rail">
        <a href="/" className="brand">
          ♜ ReignHub
        </a>
        <Button variant="primary" onClick={() => setPairDialog(true)}>
          <Plus size={16} />
          连接设备
        </Button>
        <span className="rail-caption">我的连接</span>
        <div className="device-list">
          {devices.map((d) => {
            const k = deviceKey(d),
              c = connections[k];
            return (
              <div
                className={
                  "device-row " + (selectedDevice === k ? "active" : "")
                }
                key={k}
              >
                <button
                  onClick={() => {
                    if (k !== selectedDevice) {
                      setSelected(undefined);
                      setSessions([]);
                    }
                    setSelectedDevice(k);
                    setView("sessions");
                    if (!c || c.path === "Disconnected") void connect(d);
                  }}
                >
                  <Monitor size={18} />
                  <span>
                    <strong>{d.name || "未命名设备"}</strong>
                    <small
                      className={c?.path.startsWith("P2P") ? "online" : ""}
                    >
                      {reconnecting[k] ? "连接中…" : c?.path || d.connectionId}
                    </small>
                  </span>
                </button>
                <Button
                  size="icon"
                  variant="ghost"
                  className="device-connection-status"
                  aria-label="连接详情"
                  onClick={() => setConnectionPanel(k)}
                >
                  {reconnecting[k] ? (
                    <LoaderCircle size={14} className="spin" />
                  ) : c?.path === "Disconnected" || !c ? (
                    <WifiOff size={14} />
                  ) : (
                    <Wifi size={14} />
                  )}
                </Button>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label="移除设备"
                  onClick={() => void remove(d)}
                >
                  <X size={13} />
                </Button>
              </div>
            );
          })}
          {cloudDevices
            .filter((d) => !devices.some((local) => local.id === d.id))
            .map((d) => (
              <div className="device-row" key={"cloud-" + d.id}>
                <button
                  onClick={() => {
                    setCode(d.connectionId);
                    setPairDialog(true);
                  }}
                >
                  <Monitor size={18} />
                  <span>
                    <strong>{d.name}</strong>
                    <small>账号设备 · 首次连接需授权</small>
                  </span>
                </button>
              </div>
            ))}
          {!devices.length && !cloudDevices.length && (
            <p className="empty-hint">
              连接电脑，在这里管理
              <br />
              你的 Agent 会话。
            </p>
          )}
        </div>
        <div className="rail-bottom">
          <a className="button ghost" href="/account">
            <User size={16} />
            账号与套餐
          </a>
          <Button variant="ghost" onClick={() => setSettings(true)}>
            <Settings size={16} />
            设置
          </Button>
        </div>
      </aside>
      <aside className="session-sidebar">
        <header>
          <Button
            size="icon"
            variant="ghost"
            className="mobile-only"
            aria-label="返回设备列表"
            onClick={() => setView("devices")}
          >
            <ArrowLeft size={18} />
          </Button>
          <div>
            <strong>{current?.name || "会话"}</strong>
            <small>{active?.path || "选择一个设备"}</small>
          </div>
          <Button
            size="icon"
            variant="ghost"
            aria-label="新建会话"
            onClick={() => void create()}
          >
            <Plus size={18} />
          </Button>
        </header>
        <label className="search">
          <Search size={15} />
          <input
            placeholder="搜索会话"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </label>
        <div className="session-list">
          {groups.map(([cwd, list]) => (
            <section key={cwd}>
              <h3 title={cwd}>
                <Folder size={14} />
                {cwd.split(/[\\/]/).filter(Boolean).pop() || cwd}
              </h3>
              {list.map((s) => (
                <button
                  className={
                    "session-row " +
                    (selected?.id === s.id &&
                    selected?.provider === s.providerId
                      ? "selected"
                      : "")
                  }
                  key={s.providerId + s.id}
                  onClick={() => selectSession(s)}
                >
                  <SessionIcon
                    session={s}
                    className={
                      s.providerId.includes("app")
                        ? "app-provider"
                        : "cli-provider"
                    }
                  />
                  <span>{s.title || s.preview || "新会话"}</span>
                  {isRunning(s.status) && <i className="status-dot" />}
                </button>
              ))}
            </section>
          ))}
          {!groups.length && (
            <p className="empty-hint">
              {active ? "暂无会话" : "连接设备后查看会话"}
            </p>
          )}
        </div>
      </aside>
      <main className="chat-pane">
        {selected && active ? (
          <Chat
            key={selected.device + selected.provider + selected.id}
            c={active}
            cache={cache}
            selected={selected}
            transcript={transcript}
            onOlder={async () => {
              if (!transcript?.nextCursor) return;
              const key = [
                selected.device,
                selected.provider,
                selected.id,
              ].join("|");
              const page = await active.call<Transcript>({
                method: "transcript",
                provider: selected.provider,
                session: selected.id,
                cursor: transcript.nextCursor,
              });
              if (transcriptScope.current !== key) return;
              const latest = transcriptRef.current;
              if (!latest) return;
              const ids = new Set(latest.turns.map((turn) => turn.id));
              const value = {
                ...latest,
                turns: [
                  ...page.turns.filter((turn) => !ids.has(turn.id)),
                  ...latest.turns,
                ],
                nextCursor: page.nextCursor,
              };
              transcriptRef.current = value;
              setTranscript(value);
              await cache.putSnapshot(key, value);
            }}
            onError={onError}
            onConnectionStatus={() => setConnectionPanel(selectedDevice)}
            onBack={() => {
              setView("sessions");
              if (history.state?.reignChat) history.back();
            }}
          />
        ) : (
          <div className="welcome">
            <Monitor size={34} />
            <h1>{current?.name || "随时接续你的工作"}</h1>
            <p>
              {active
                ? "从左侧选择会话，或新建一个会话。"
                : "输入电脑上的六位设备编号，连接你的 Agent。"}
            </p>
            <Button onClick={() => setPairDialog(true)}>
              <LinkIcon size={16} />
              连接设备
            </Button>
            {providers.map((p) => (
              <div className="provider-status" key={p.id}>
                <Terminal size={16} />
                <span>{p.name}</span>
                <small>{p.controllable ? "已就绪" : p.detail}</small>
              </div>
            ))}
          </div>
        )}
        <ErrorNotice message={error} onClose={() => setError("")} />
      </main>
      <Dialog
        open={pairDialog}
        onOpenChange={(v) => {
          if (!v) pairAbort.current?.abort();
          setPairDialog(v);
        }}
        title="连接设备"
      >
        <p className="muted">
          输入被控电脑上显示的设备编号，首次连接需要获得授权。
        </p>
        <label>
          设备编号
          <input
            className="field device-code"
            placeholder="000 000"
            value={code}
            onChange={(e) =>
              setCode(e.target.value.replace(/[^0-9]/g, "").slice(0, 6))
            }
            inputMode="numeric"
            autoFocus
          />
        </label>
        <label>
          访问密码（如已设置）
          <input
            className="field"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !pairing) void pair();
            }}
          />
        </label>
        <small className="muted">服务：{service}</small>

        <Button
          variant="primary"
          disabled={code.length !== 6 || pairing}
          onClick={() => void pair()}
        >
          {pairing ? (
            <LoaderCircle className="spin" size={16} />
          ) : (
            <LinkIcon size={16} />
          )}
          连接
        </Button>
      </Dialog>
      <ConnectionPanel
        open={!!connectionPanel}
        attempt={
          attempts[connectionPanel] ||
          (() => {
            const d = devices.find((d) => deviceKey(d) === connectionPanel);
            return d
              ? {
                  name: d.name,
                  service: d.service,
                  code: d.connectionId,
                  started: Date.now(),
                  active: false,
                  steps: [],
                  path: connections[connectionPanel]?.path || "尚未连接",
                }
              : undefined;
          })()
        }
        onClose={() => setConnectionPanel("")}
        onCancel={() => cancelAttempt(connectionPanel)}
        onRetry={() => {
          if (connectionPanel === "pair") void pair();
          else {
            const d = devices.find((d) => deviceKey(d) === connectionPanel);
            if (d) void connect(d, false, true);
          }
        }}
      />
      <Dialog title="设置" open={settings} onOpenChange={setSettings}>
        <p className="muted">当前服务：{service}</p>
        <p className="muted">
          Web 使用当前站点连接。设备密钥、消息和附件缓存在此浏览器中。
        </p>
        <hr />
        <Button
          variant="destructive"
          onClick={async () => {
            if (
              confirm(
                "清除此浏览器保存的设备密钥、消息和附件？被控端授权仍需在电脑上撤销。",
              )
            ) {
              await cache.delete();
              location.reload();
            }
          }}
        >
          <Trash2 size={16} />
          清除本地数据
        </Button>
      </Dialog>
    </div>
  );
}
export function Chat({
  c,
  cache,
  selected,
  transcript: t,
  onOlder,
  onError,
  onConnectionStatus,
  onBack,
}: {
  c: Connection;
  cache: ReignHubCache;
  selected: Selected;
  transcript?: Transcript;
  onOlder: () => Promise<void>;
  onConnectionStatus: () => void;
  onError: (e: unknown) => void;
  onBack: () => void;
}) {
  const [text, setText] = useState("");
  const [model, setModel] = useState("");
  const [models, setModels] = useState<
    { id: string; name: string; default: boolean }[]
  >([]);
  const [uploads, setUploads] = useState<{ file: File; url?: string }[]>([]);
  const [sending, setSending] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [copied, setCopied] = useState("");
  const [approvals, setApprovals] = useState<
    { id: string; method: string; params: unknown }[]
  >([]);
  const [dragging, setDragging] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const scroll = useRef<HTMLDivElement>(null);
  const bottom = useRef(true);
  const connected = c.path !== "Disconnected";
  const running = !!activeTurn(t);
  const rows = useMemo(() => messages(t), [t]);
  const [atBottom, setAtBottom] = useState(true);
  const content = useRef<HTMLDivElement>(null);
  const initialBottom = useRef(true);
  const v = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scroll.current,
    estimateSize: () => 120,
    overscan: 6,
    paddingStart: t?.nextCursor ? 44 : 0,
    getItemKey: (i) => rows[i].id,
    // Avoid moving the viewport when rows below the reading position change.
  });
  useEffect(() => {
    void c
      .call<{ id: string; name: string; default: boolean }[]>({
        method: "models",
        provider: selected.provider,
      })
      .then((ms) => {
        setModels(ms);
        setModel(
          t?.session.model || ms.find((m) => m.default)?.id || ms[0]?.id || "",
        );
      })
      .catch(() => {});
    let active = true;
    const timer = setInterval(() => {
      void c
        .call<{ id: string; method: string; params: unknown }[]>({
          method: "approvals",
          provider: selected.provider,
        })
        .then((a) => {
          if (active) setApprovals(a);
        })
        .catch(() => {});
    }, 2000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [c, selected.provider]);
  useEffect(() => {
    if (t?.session.model) setModel(t.session.model);
  }, [t?.session.model]);
  useEffect(() => {
    bottom.current = true;
    initialBottom.current = true;
    setAtBottom(true);
  }, [selected.id, selected.provider]);
  useEffect(() => {
    if (bottom.current && rows.length) {
      v.scrollToIndex(rows.length - 1, { align: "end" });
    }
  }, [rows, v]);
  useEffect(() => {
    const el = scroll.current,
      inner = content.current;
    if (!el || !inner) return;
    const observer = new ResizeObserver(() => {
      if (bottom.current || initialBottom.current) {
        el.scrollTop = el.scrollHeight;
        if (rows.length) initialBottom.current = false;
      }
    });
    observer.observe(el);
    observer.observe(inner);
    return () => observer.disconnect();
  }, [rows.length > 0]);
  const urlsRef = useRef(uploads);
  urlsRef.current = uploads;
  useEffect(
    () => () => {
      for (const f of urlsRef.current) if (f.url) URL.revokeObjectURL(f.url);
    },
    [],
  );
  function add(files: File[]) {
    if (uploads.length + files.length > 8) {
      onError("每次最多上传 8 个附件");
      return;
    }
    for (const file of files)
      if (file.size > 50 * 1024 * 1024) {
        onError("单个文件不能超过 50 MB");
        return;
      }
    setUploads((x) => [
      ...x,
      ...files.map((file) => ({
        file,
        url: file.type.startsWith("image/")
          ? URL.createObjectURL(file)
          : undefined,
      })),
    ]);
  }
  async function send() {
    if (!connected || sending || (!text.trim() && !uploads.length)) return;
    setSending(true);
    try {
      const attachments: string[] = [];
      for (const u of uploads) {
        const a = await upload(c, selected.provider, selected.id, u.file);
        attachments.push(a.id);
      }
      await c.call({
        method: running ? "steer" : "send",
        provider: selected.provider,
        session: selected.id,
        text,
        model,
        turn: t?.session.activeTurnId,
        attachments,
      });
      setText("");
      for (const u of uploads) if (u.url) URL.revokeObjectURL(u.url);
      setUploads([]);
      bottom.current = true;
    } catch (e) {
      const message = errorText(e);
      onError(
        message.includes("就绪") || message.includes("read-only")
          ? "此会话尚未接入控制。请在被控电脑打开 ReignHub，在 Agent 状态面板点击「启用／通过 ReignHub 启动 Codex App」，再重新连接。\n" +
              message
          : message,
      );
    } finally {
      setSending(false);
    }
  }
  async function copy(item: Item) {
    try {
      await copyText(readableMessage(item));
      setCopied(item.id);
      setTimeout(() => setCopied(""), 1800);
    } catch (e) {
      onError(e);
    }
  }
  return (
    <>
      <header className="chat-header">
        <Button
          size="icon"
          variant="ghost"
          className="mobile-only"
          onClick={onBack}
          aria-label="返回会话"
        >
          <ArrowLeft size={18} />
        </Button>
        <div>
          <strong>{t?.session.title || "会话"}</strong>
          <small>{t?.session.cwd}</small>
        </div>
        <button
          type="button"
          onClick={onConnectionStatus}
          aria-label="连接详情"
          className={"path-badge " + (c.path.startsWith("P2P") ? "direct" : "")}
        >
          {c.path}
        </button>
      </header>
      <div className="messages-area">
        <div
          className="messages"
          ref={scroll}
          onScroll={() => {
            const el = scroll.current!;
            bottom.current =
              el.scrollHeight - el.scrollTop - el.clientHeight < 48;
            setAtBottom(bottom.current);
          }}
        >
          <div
            ref={content}
            className="message-virtual-content"
            style={{
              height: v.getTotalSize(),
              position: "relative",
              width: "100%",
            }}
          >
            {t?.nextCursor && (
              <div className="load-older">
                <Button
                  variant="ghost"
                  size="small"
                  disabled={loadingOlder || !connected}
                  onClick={async () => {
                    bottom.current = false;
                    setLoadingOlder(true);
                    try {
                      await onOlder();
                    } catch (e) {
                      onError(e);
                    } finally {
                      setLoadingOlder(false);
                    }
                  }}
                >
                  {loadingOlder ? "加载中…" : "加载更早消息"}
                </Button>
              </div>
            )}
            {v.getVirtualItems().map((row) => {
              const item = rows[row.index];
              return (
                <div
                  key={row.key}
                  data-index={row.index}
                  ref={v.measureElement}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    transform: `translateY(${row.start}px)`,
                  }}
                >
                  <article
                    data-message-role={item.role}
                    className={
                      "message " +
                      (item.role === "activity"
                        ? "activity-message"
                        : item.role === "user"
                          ? "user-message"
                          : "assistant-message")
                    }
                  >
                    <div className="message-content">
                      {item.role === "activity" ? (
                        <Activity
                          tools={item.tools || []}
                          running={connected}
                        />
                      ) : (
                        <>
                          {!!item.attachments?.length && (
                            <Attachments
                              items={item.attachments}
                              c={c}
                              cache={cache}
                              provider={selected.provider}
                              session={selected.id}
                              onError={onError}
                            />
                          )}
                          <MessageContent role={item.role} text={item.text} />
                          {!item.live && (
                            <div className="message-actions">
                              <Button
                                variant="ghost"
                                size="icon"
                                aria-label="复制完整消息"
                                onClick={() => void copy(item)}
                              >
                                {copied === item.id ? (
                                  <Check size={14} />
                                ) : (
                                  <Copy size={14} />
                                )}
                              </Button>
                              {item.timestampMs && (
                                <time
                                  title={new Date(
                                    item.timestampMs,
                                  ).toLocaleString()}
                                >
                                  {new Date(
                                    item.timestampMs,
                                  ).toLocaleTimeString([], {
                                    hour: "2-digit",
                                    minute: "2-digit",
                                  })}
                                </time>
                              )}
                            </div>
                          )}
                        </>
                      )}
                    </div>
                  </article>
                </div>
              );
            })}
          </div>
          {connected && showThinking(t) && (
            <div className="thinking shimmer">Thinking…</div>
          )}
        </div>
        <div className="message-nav">
          {navigationRows(rows).map((item) => (
            <button
              key={item.id}
              title={readableMessage(item).slice(0, 100)}
              aria-label="跳转到消息"
              onClick={() => {
                bottom.current = false;
                setAtBottom(false);
                v.scrollToIndex(
                  rows.findIndex((r) => r.id === item.id),
                  { align: "center" },
                );
              }}
            >
              <span />
            </button>
          ))}
        </div>
        {!atBottom && (
          <Button
            className="jump-latest"
            variant="default"
            size="icon"
            aria-label="回到最新"
            onClick={() => {
              bottom.current = true;
              setAtBottom(true);
              v.scrollToIndex(rows.length - 1, { align: "end" });
            }}
          >
            <ArrowDown size={16} />
          </Button>
        )}
      </div>
      {approvals.length > 0 && (
        <div className="approval-card">
          {approvals.map((a) => (
            <div key={a.id}>
              <Shield size={16} />
              <span>Agent 请求授权：{a.method}</span>
              <Button
                onClick={async () => {
                  try {
                    await c.call({
                      method: "decide",
                      provider: selected.provider,
                      approval: a.id,
                      decision: { decision: "decline" },
                    });
                    setApprovals((x) => x.filter((p) => p.id !== a.id));
                  } catch (e) {
                    onError(e);
                  }
                }}
              >
                拒绝
              </Button>
              <Button
                variant="primary"
                onClick={async () => {
                  try {
                    await c.call({
                      method: "decide",
                      provider: selected.provider,
                      approval: a.id,
                      decision: { decision: "accept" },
                    });
                    setApprovals((x) => x.filter((p) => p.id !== a.id));
                  } catch (e) {
                    onError(e);
                  }
                }}
              >
                允许
              </Button>
            </div>
          ))}
        </div>
      )}
      {!connected && (
        <div className="connection-cache-note" role="status">
          设备离线，显示本地缓存；连接恢复后同步最新消息。
        </div>
      )}
      <div
        className={"composer-wrap " + (dragging ? "dragging" : "")}
        onDragOver={(e) => {
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          add([...e.dataTransfer.files]);
        }}
      >
        <div className="composer">
          {uploads.length > 0 && (
            <div className="upload-previews">
              {uploads.map((u, i) => (
                <div key={i}>
                  {u.url ? (
                    <img src={u.url} alt="待上传图片" />
                  ) : (
                    <span>{u.file.name}</span>
                  )}
                  <button
                    aria-label="移除附件"
                    onClick={() => {
                      if (u.url) URL.revokeObjectURL(u.url);
                      setUploads((x) => x.filter((_, n) => n !== i));
                    }}
                  >
                    <X size={12} />
                  </button>
                </div>
              ))}
            </div>
          )}
          <textarea
            placeholder={
              running ? "添加后续指令…" : "发送消息，或拖入图片与文件"
            }
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={2}
            onPaste={(e) => {
              const files = [...e.clipboardData.files];
              if (files.length) {
                e.preventDefault();
                add(files);
              }
            }}
            onKeyDown={(e) => {
              if (
                e.key === "Enter" &&
                !e.shiftKey &&
                !e.nativeEvent.isComposing
              ) {
                e.preventDefault();
                void send();
              }
            }}
          />
          <div className="composer-toolbar">
            <Button
              size="icon"
              variant="ghost"
              aria-label="上传图片或文件"
              onClick={() => fileRef.current?.click()}
            >
              <Plus size={20} />
            </Button>
            <input
              type="file"
              multiple
              hidden
              ref={fileRef}
              onChange={(e) => {
                add([...(e.target.files || [])]);
                e.target.value = "";
              }}
            />
            <span className="composer-spacer" />
            <select
              aria-label="模型"
              value={model}
              onChange={(e) => setModel(e.target.value)}
            >
              {model && !models.some((m) => m.id === model) && (
                <option value={model}>{model}</option>
              )}
              {models.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name || m.id}
                </option>
              ))}
            </select>
            {running && connected && (
              <Button
                size="icon"
                variant="ghost"
                aria-label="停止生成"
                onClick={() =>
                  void c
                    .call({
                      method: "interrupt",
                      provider: selected.provider,
                      session: selected.id,
                      turn: t?.session.activeTurnId,
                    })
                    .catch(onError)
                }
              >
                <Square size={15} />
              </Button>
            )}
            <Button
              variant="primary"
              size="icon"
              disabled={
                !connected || sending || (!text.trim() && !uploads.length)
              }
              aria-label="发送"
              onClick={() => void send()}
            >
              {sending ? (
                <LoaderCircle className="spin" size={16} />
              ) : (
                <ArrowUp size={18} />
              )}
            </Button>
          </div>
        </div>
        <small className="composer-caption">
          Enter 发送 · Shift + Enter 换行
        </small>
      </div>
    </>
  );
}

export function Activity({
  tools,
  running = true,
}: {
  tools: Item[];
  running?: boolean;
}) {
  return (
    <details className="activity">
      <summary>
        <Wrench size={14} className="activity-icon" />
        <span className={running ? "shimmer" : ""}>{activityLabel(tools)}</span>
        <ChevronDown size={14} className="activity-chevron" />
      </summary>
      <ul>
        {tools.flatMap((tool) =>
          (tool.activities?.length
            ? tool.activities
            : [
                {
                  title:
                    tool.status === "completed" ? "操作已完成" : "正在执行…",
                  detail: undefined,
                },
              ]
          ).map((a, i) => (
            <li key={`${tool.id}|${i}`}>
              <span>{a.title}</span>
              {a.detail && <small>{a.detail}</small>}
            </li>
          )),
        )}
      </ul>
    </details>
  );
}

function SessionIcon({
  session,
  className,
}: {
  session: Session;
  className?: string;
}) {
  const Icon = session.providerId.includes("claude")
    ? Sparkles
    : session.sourceKind === "app" || session.providerId === "codex-app"
      ? Monitor
      : Terminal;
  return <Icon size={14} className={className} />;
}
