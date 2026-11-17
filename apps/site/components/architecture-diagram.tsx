import { type Locale } from "../lib/i18n";
import "./architecture-diagram.css";

type DiagramKind = "overview" | "connection" | "codex" | "stack";
export function ArchitectureDiagram({
  locale = "en",
  kind = "overview",
}: {
  locale?: Locale;
  kind?: DiagramKind;
}) {
  const zh = locale === "zh-CN";
  const label = (cn: string, en: string) => (zh ? cn : en);
  const id = `architecture-${kind}-${locale}`;
  const titles = {
    overview: label("ReignHub 整体架构", "ReignHub system architecture"),
    connection: label(
      "从配对到加密对话",
      "From pairing to encrypted conversations",
    ),
    codex: label(
      "Codex App 本地 IPC 远控",
      "Codex App remote control over local IPC",
    ),
    stack: label("ReignHub 技术分层", "ReignHub technology layers"),
  };
  const captions = {
    overview: label(
      "云端协调连接，设备间优先直连；Agent 和项目始终运行在你的电脑上。",
      "The cloud coordinates connections. Devices connect directly first; agents and projects run on your computer.",
    ),
    connection: label(
      "首次授权建立信任，直连与中继使用同一套端到端加密。",
      "First-time authorization establishes trust. Both direct and relay paths use end-to-end encryption.",
    ),
    codex: label(
      "现有会话跟随原 App；远程新建会话由 ReignHub 的私有后端执行。",
      "Existing sessions follow their App owner; new remote sessions use ReignHub’s private backend.",
    ),
    stack: label(
      "原生与 Web 共用连接协议，云端负责协调，本地保存会话缓存。",
      "Native and Web share a connection protocol. The cloud coordinates; devices cache sessions locally.",
    ),
  };
  function Node({
    x,
    y,
    w = 180,
    title,
    detail,
    accent = false,
  }: {
    x: number;
    y: number;
    w?: number;
    title: string;
    detail: string;
    accent?: boolean;
  }) {
    return (
      <g className={accent ? "diagram-node diagram-accent" : "diagram-node"}>
        <rect x={x} y={y} width={w} height={76} rx={12} />
        <text
          x={x + w / 2}
          y={y + 31}
          textAnchor="middle"
          className="diagram-title"
        >
          {title}
        </text>
        <text
          x={x + w / 2}
          y={y + 53}
          textAnchor="middle"
          className="diagram-detail"
        >
          {detail}
        </text>
      </g>
    );
  }
  function Arrow({ d, dashed = false }: { d: string; dashed?: boolean }) {
    return (
      <path
        className="diagram-line"
        d={d}
        fill="none"
        strokeDasharray={dashed ? "5 5" : undefined}
        markerEnd={`url(#${id}-arrow)`}
      />
    );
  }
  return (
    <figure className="architecture-diagram">
      <div
        className="diagram-scroll"
        tabIndex={0}
        role="region"
        aria-label={titles[kind]}
      >
        <svg
          viewBox={`0 0 740 ${kind === "codex" ? 250 : kind === "connection" ? 450 : 390}`}
          role="img"
          aria-labelledby={`${id}-title ${id}-description`}
        >
          <title id={`${id}-title`}>{titles[kind]}</title>
          <desc id={`${id}-description`}>{captions[kind]}</desc>
          <defs>
            <marker
              id={`${id}-arrow`}
              markerWidth="7"
              markerHeight="7"
              refX="6"
              refY="3.5"
              orient="auto-start-reverse"
            >
              <path d="M0 0 L7 3.5 L0 7" className="diagram-arrow" />
            </marker>
          </defs>
          {kind === "overview" && (
            <>
              <Node
                x={260}
                y={22}
                w={220}
                title="Cloudflare Worker"
                detail={label(
                  "发现 · 配对 · 信令 · 加密中继",
                  "Discovery · pairing · signaling · relay",
                )}
                accent
              />
              <Node
                x={530}
                y={22}
                w={180}
                title="D1 + Durable Objects"
                detail={label("元数据 · 设备房间", "Metadata · device rooms")}
              />
              <Arrow d="M480 60 H523" />
              <Node
                x={30}
                y={198}
                title={label("控制端", "Controller")}
                detail={label("桌面 · 手机 · 浏览器", "Desktop · mobile · Web")}
              />
              <Node
                x={310}
                y={198}
                title={label("被控电脑 · ReignHub", "Host computer · ReignHub")}
                detail={label("Go 核心 · Provider", "Go core · providers")}
                accent
              />
              <Node
                x={550}
                y={198}
                w={160}
                title="Agent"
                detail={label(
                  "Codex App / CLI · 项目",
                  "Codex App / CLI · projects",
                )}
              />
              <Arrow d="M300 98 V138 H120 V191" dashed />
              <Arrow d="M430 98 V138 H400 V191" dashed />
              <text
                x={369}
                y={129}
                textAnchor="middle"
                className="diagram-detail"
              >
                {label("连接协调 / 中继回退", "Coordination / relay fallback")}
              </text>
              <path
                d="M218 236 H302"
                className="diagram-line diagram-direct"
                markerStart={`url(#${id}-arrow)`}
                markerEnd={`url(#${id}-arrow)`}
              />
              <text
                x={260}
                y={217}
                textAnchor="middle"
                className="diagram-detail"
              >
                P2P
              </text>
              <Arrow d="M490 236 H542" />
              <Node
                x={30}
                y={302}
                w={220}
                title={label(
                  "公共 / 自建探测器",
                  "Public / self-hosted probes",
                )}
                detail="UDP STUN · TCP :26331"
              />
              <Arrow d="M250 338 H400 V281" dashed />
              <text
                x={305}
                y={327}
                textAnchor="middle"
                className="diagram-detail"
              >
                {label("候选地址", "Candidates")}
              </text>
            </>
          )}
          {kind === "connection" && (
            <>
              {[
                label("控制端", "Controller"),
                label("连接服务", "Connection service"),
                label("被控电脑", "Host computer"),
              ].map((name, i) => (
                <g key={name}>
                  <Node
                    x={30 + i * 260}
                    y={18}
                    w={160}
                    title={name}
                    detail={i === 1 ? "Worker" : "ReignHub"}
                    accent={i === 1}
                  />
                  <path
                    d={`M${110 + i * 260} 100 V425`}
                    className="diagram-lifeline"
                  />
                </g>
              ))}
              <Arrow d="M110 137 H363" />
              <text
                x={240}
                y={125}
                textAnchor="middle"
                className="diagram-detail"
              >
                {label(
                  "1 · 发现设备与请求配对",
                  "1 · Discover and request pairing",
                )}
              </text>
              <Arrow d="M370 185 H623" />
              <text
                x={500}
                y={173}
                textAnchor="middle"
                className="diagram-detail"
              >
                {label(
                  "2 · 电脑确认 / 密码验证",
                  "2 · Host approval / password",
                )}
              </text>
              <path
                d="M117 250 H623"
                className="diagram-line"
                markerStart={`url(#${id}-arrow)`}
                markerEnd={`url(#${id}-arrow)`}
              />
              <text
                x={370}
                y={238}
                textAnchor="middle"
                className="diagram-detail"
              >
                {label(
                  "3 · 交换候选地址，尝试直连",
                  "3 · Exchange candidates, establish direct path",
                )}
              </text>
              <path
                d="M117 318 H623"
                className="diagram-line diagram-direct"
                markerStart={`url(#${id}-arrow)`}
                markerEnd={`url(#${id}-arrow)`}
              />
              <text
                x={370}
                y={306}
                textAnchor="middle"
                className="diagram-title"
              >
                {label(
                  "4 · 端到端加密 RPC 与增量输出",
                  "4 · E2E encrypted RPC and live output",
                )}
              </text>
              <Arrow d="M110 390 H363" dashed />
              <Arrow d="M370 390 H623" dashed />
              <text
                x={370}
                y={378}
                textAnchor="middle"
                className="diagram-detail"
              >
                {label(
                  "直连不可用 → 服务加密中继",
                  "Direct unavailable → encrypted service relay",
                )}
              </text>
            </>
          )}
          {kind === "codex" && (
            <>
              <Node
                x={25}
                y={22}
                w={190}
                title="Codex App"
                detail={label("原有会话的 owner", "Owner of App sessions")}
              />
              <Node
                x={25}
                y={180}
                w={190}
                title="ReignHub"
                detail={label("远程会话的 owner", "Owner of remote sessions")}
              />
              <Node
                x={300}
                y={100}
                w={190}
                title={label("本地 IPC", "Local IPC")}
                detail="Owner ↔ Follower"
                accent
              />
              <Node
                x={560}
                y={22}
                w={170}
                title={label("App 私有后端", "App backend")}
                detail="Codex app-server"
              />
              <Node
                x={560}
                y={180}
                w={170}
                title={label("Hub 私有后端", "Hub backend")}
                detail="stdio app-server"
              />
              <Arrow d="M215 60 H270 V120 H292" />
              <Arrow d="M300 150 H270 V218 H223" />
              <Arrow d="M215 218 H250 V150 H292" />
              <Arrow d="M300 120 H250 V60 H223" />
              <Arrow d="M215 42 H552" />
              <Arrow d="M215 240 H552" />
            </>
          )}
          {kind === "stack" && (
            <>
              <Node
                x={30}
                y={20}
                w={320}
                title={label("原生桌面与移动端", "Native desktop & mobile")}
                detail="Go + NSGo · SQLite"
              />
              <Node
                x={390}
                y={20}
                w={320}
                title={label("浏览器控制端", "Web controller")}
                detail="NSGo + WebAssembly · IndexedDB"
              />
              <Arrow d="M190 96 V128 H320 V149" />
              <Arrow d="M550 96 V128 H420 V149" />
              <Node
                x={170}
                y={157}
                w={400}
                title={label(
                  "统一连接与加密协议",
                  "Shared connection & encryption protocol",
                )}
                detail="WebRTC / UDP · Native TCP · Encrypted WSS"
                accent
              />
              <Arrow d="M320 233 V258 H190 V285" />
              <Arrow d="M420 233 V258 H550 V285" />
              <Node
                x={30}
                y={293}
                w={320}
                title={label("本机 Agent 层", "Local agent layer")}
                detail="Go Provider · Codex app-server"
              />
              <Node
                x={390}
                y={293}
                w={320}
                title={label("云端连接协调", "Cloud connection coordination")}
                detail="Workers · Durable Objects · D1"
              />
            </>
          )}
        </svg>
      </div>
      <figcaption>{captions[kind]}</figcaption>
    </figure>
  );
}
