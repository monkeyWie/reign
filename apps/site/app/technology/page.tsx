import type { Metadata } from "next";
import Link from "next/link";
import {
  ArrowDown,
  ArrowRight,
  Cloud,
  Code2,
  Database,
  Globe,
  Network,
  ShieldCheck,
  Terminal,
} from "lucide-react";
import {
  BottomCTA,
  PageIntro,
  SectionHeading,
  SiteShell,
} from "../../components/site";
export const metadata: Metadata = {
  title: "技术栈 · Reign",
  description:
    "了解 NSGo 原生 UI、Go 核心、React Web、WebRTC／TCP、Cloudflare Worker、D1 与端到端加密。",
};
const layers = [
  {
    id: "native",
    icon: Code2,
    kicker: "01 / NATIVE CLIENTS",
    title: "Go + NSGo，原生到底。",
    description:
      "桌面与手机共用 Go 编写的 UI，映射到各平台原生视图。应用核心直接在同一进程内调用，不需要额外启动一个 Hub 后台 CLI，也不内嵌 WebView。",
    tags: ["macOS · Windows · Linux", "Android · iOS", "原生视图"],
    points: [
      [
        "UI 与核心同语言",
        "设备管理、配对、网络和 Provider 复用 Go 逻辑。Agent 自身的独立进程不被强行塞进 UI 进程。",
      ],
      [
        "长会话更从容",
        "原生虚拟消息列表、增量 Markdown、按会话隔离的草稿与附件预览。",
      ],
    ],
    url: "https://nsgo.dev/",
    label: "了解 NSGo",
  },
  {
    id: "web",
    icon: Globe,
    kicker: "02 / WEB EXPERIENCE",
    title: "React + Next.js，打开就能连接。",
    description:
      "官网、价格、账号和 Web 控制台共用 Next.js App Router，通过 OpenNext 适配到 Worker。浏览器直接使用原生 WebRTC 与 Web Crypto，不必先下载 Go WASM。",
    tags: ["Next.js + React", "OpenNext", "Fumadocs + MDX"],
    points: [
      [
        "文档也是代码",
        "Fumadocs 从 Markdown／MDX 生成目录、搜索和页面，文档与开源核心一起维护。",
      ],
      [
        "标准 Web 能力",
        "IndexedDB 本地缓存、虚拟消息列表、响应式布局。浏览器作为控制端，不直接访问本机 Agent 进程。",
      ],
    ],
    url: "https://fumadocs.dev",
    label: "了解 Fumadocs",
  },
  {
    id: "network",
    icon: Network,
    kicker: "03 / CONNECTIVITY",
    title: "WebRTC + TCP，尽可能直连。",
    description:
      "原生端通过 Pion WebRTC 尝试 UDP 数据通道，同时尝试原生 TCP 打洞；浏览器使用标准 RTCPeerConnection。公共与可选自建 STUN 并行收集候选。",
    tags: ["WebRTC DataChannel", "原生 TCP", "WSS fallback"],
    points: [
      [
        "一个地址，发现能力",
        "客户端只填服务根地址，探测地址从 /p2p/config 下发。可选 Go 探测器在同一 26330 端口监听 TCP 和 UDP。",
      ],
      [
        "失败后有明确回退",
        "自建服务提供自己的加密 WebSocket 中继；官方中继受套餐控制。当前不依赖 TURN、DERP 或 Tunnel。",
      ],
    ],
    url: "/docs/guides/connections",
    label: "阅读连接策略",
  },
  {
    id: "cloud",
    icon: Cloud,
    kicker: "04 / CONNECTION SERVICE",
    title: "一个 Worker，协调所有连接。",
    description:
      "Cloudflare Worker 承载网站、Web 客户端和 /p2p/*。Durable Objects 为设备房间提供有状态协调与可休眠 WebSocket，D1 持久化设备与配对元数据。",
    tags: ["Cloudflare Workers", "Durable Objects", "D1"],
    points: [
      [
        "少一点运维拼装",
        "默认 workers.dev 提供 HTTPS；自建无需准备 OAuth、支付或一台必需的公网 VM。探测器按网络需要添加。",
      ],
      [
        "复用核心，隔离业务",
        "开源连接层不执行商业配额；官方业务层注入身份、计费和服务端权益策略，复用同一连接协议。",
      ],
    ],
    url: "/docs/self-hosting",
    label: "部署自己的 Worker",
  },
  {
    id: "data",
    icon: Database,
    kicker: "05 / LOCAL-FIRST STORAGE",
    title: "本地有缓存，云端有边界。",
    description:
      "原生端使用 SQLite 与附件文件，浏览器使用 IndexedDB 保存配对身份和会话快照。断线恢复时补齐状态，再继续增量事件。",
    tags: ["SQLite", "IndexedDB", "D1 + Drizzle"],
    points: [
      [
        "缓存不是云托管",
        "云端不保存聊天与附件明文；原始项目和 Agent 继续运行在被控电脑上。",
      ],
      [
        "数据库各司其职",
        "官方 D1 业务层使用 Drizzle ORM；Go 的本地存储和部分 GORM 路径保持独立，避免误把两种服务数据库混为一谈。",
      ],
    ],
    url: "/docs/architecture/stack",
    label: "查看完整技术说明",
  },
  {
    id: "security",
    icon: ShieldCheck,
    kicker: "06 / TRUST & SECURITY",
    title: "密钥在设备上，授权有边界。",
    description:
      "配对通过 X25519／HKDF／AES-GCM 建立信任，业务通信使用独立方向的 AEAD 密钥并防重放。经中继的消息仍为端到端密文。",
    tags: ["端到端加密", "本地设备信任", "短期签名权益"],
    points: [
      [
        "编号不是凭据",
        "六位设备编号用于发现；首次访问需要被控方确认或验证访问密码。登录同步设备目录，不会替新控制端自动建立配对信任。",
      ],
      [
        "官方商业能力",
        "Better Auth 对接 GitHub／Google，Waffo 对接支付。官方权益通过短期签名授权在被控端验证，直连不会绕过限制。",
      ],
    ],
    url: "/docs/privacy",
    label: "安全与隐私边界",
  },
];
export default function Technology() {
  return (
    <SiteShell>
      <main id="main">
        <PageIntro
          eyebrow="ENGINEERED TO STAY OUT OF YOUR WAY"
          title={
            <>
              原生的轻量，
              <br />
              <span>开放的连接。</span>
            </>
          }
        >
          UI 用原生视图，核心交给 Go，Web
          使用浏览器标准能力。每一层都有清楚的职责，尽量少一点运行时和部署负担。
        </PageIntro>
        <section
          className="site-width architecture-panel"
          aria-label="架构概览"
        >
          <div className="architecture-clients">
            <span>
              <Code2 size={20} /> 原生桌面／手机 <small>Go + NSGo</small>
            </span>
            <span>
              <Globe size={20} /> Web 控制端 <small>React + Next.js</small>
            </span>
          </div>
          <div className="architecture-arrow">
            <ArrowDown size={20} />
            <span>设备发现 · 配对 · 信令</span>
          </div>
          <div className="architecture-service">
            <Cloud size={24} />
            <strong>Cloudflare Worker</strong>
            <span>D1 · Durable Objects · 加密中继</span>
          </div>
          <div className="architecture-arrow">
            <ArrowDown size={20} />
            <span>优先直连 / 按服务权益回退中继</span>
          </div>
          <div className="architecture-host">
            <MonitorIcon />
            <strong>被控电脑 · Go 核心</strong>
            <span>Provider → Codex App / CLI → 本地项目</span>
          </div>
        </section>
        <div className="tech-toc site-width">
          {layers.map((l) => (
            <a key={l.id} href={`#${l.id}`}>
              {l.kicker.split(" / ")[1]} <ArrowDown size={12} />
            </a>
          ))}
        </div>
        <div className="site-width tech-layers">
          {layers.map((layer) => (
            <section key={layer.id} id={layer.id} className="tech-layer">
              <div>
                <span className="site-eyebrow">{layer.kicker}</span>
                <h2>{layer.title}</h2>
                <p>{layer.description}</p>
                <div className="tech-tags">
                  {layer.tags.map((tag) => (
                    <span key={tag}>{tag}</span>
                  ))}
                </div>
                <Link className="site-text-link" href={layer.url}>
                  {layer.label} <ArrowRight size={15} />
                </Link>
              </div>
              <div className="tech-layer-details">
                <layer.icon size={30} />
                {layer.points.map(([title, body]) => (
                  <div key={title}>
                    <h3>{title}</h3>
                    <p>{body}</p>
                  </div>
                ))}
              </div>
            </section>
          ))}
        </div>
        <section className="site-width performance-section">
          <SectionHeading
            eyebrow="MEASURED, NOT GUESSED"
            title="轻量，也要把测试条件说清楚。"
          >
            不打包浏览器引擎，把资源留给真正执行工作的
            Agent。数字来自本地预览构建，随版本、平台与会话规模变化。
          </SectionHeading>
          <div className="performance-grid">
            <div>
              <strong>
                8.9 <span>MB</span>
              </strong>
              <p>macOS arm64 压缩下载体积</p>
            </div>
            <div>
              <strong>
                23.1 <span>MB</span>
              </strong>
              <p>macOS .app 文件总大小</p>
            </div>
            <div>
              <strong>
                约 119 <span>MiB</span>
              </strong>
              <p>演示面板空闲 RSS，非峰值</p>
            </div>
          </div>
          <p className="measurement-note">
            2026-11-14 · Apple M5 · macOS 26.6 · 0.1.0-site-preview ·
            隔离演示，不运行真实 Agent。RSS 不含独立的
            Codex／模型进程，不代表实际会话或全平台的内存上限。
          </p>
          <Link
            className="site-text-link"
            href="/docs/architecture/performance"
          >
            完整测量条件与复现方式 <ArrowRight size={15} />
          </Link>
        </section>
        <BottomCTA />
      </main>
    </SiteShell>
  );
}
function MonitorIcon() {
  return <Terminal size={22} />;
}
