import { ArchitectureDiagram } from "../../../components/architecture-diagram";
import { pageMetadata, marketingMeta } from "../../../lib/seo";
import { translate, isLocale } from "../../../lib/i18n";
import { notFound } from "next/navigation";
import { LocalizedLink as Link } from "../../../components/locale";
import {
  ArrowDown,
  ArrowRight,
  Cloud,
  Code2,
  Database,
  Globe,
  Network,
  ShieldCheck,
} from "lucide-react";
import {
  BottomCTA,
  PageIntro,
  SectionHeading,
  SiteShell,
} from "../../../components/site";

export default async function Technology({
  params,
}: {
  params: Promise<{ lang: string }>;
}) {
  const { lang } = await params;
  if (!isLocale(lang)) notFound();
  const t = translate(lang);
  const layers = [
    {
      id: "native",
      icon: Code2,
      kicker: "01 / NATIVE CLIENTS",
      title: t("Go + NSGo，原生到底。"),
      description: t(
        "以 Go 编写跨平台 UI，映射到系统原生视图。界面与应用核心在同一进程协作，以小体积和低内存承载完整体验。",
      ),
      tags: ["macOS · Windows · Linux", "Android · iOS", t("原生视图")],
      points: [
        [
          t("UI 与核心同语言"),
          t(
            "设备管理、配对、网络与 Provider 共用 Go 核心，原生视图与业务逻辑直接协作。",
          ),
        ],
        [
          t("长会话更从容"),
          t("原生虚拟消息列表、增量 Markdown、按会话隔离的草稿与附件预览。"),
        ],
      ],
      url: "https://nsgo.dev/",
      label: t("了解 NSGo"),
    },
    {
      id: "web",
      icon: Globe,
      kicker: "02 / WEB EXPERIENCE",
      title: t("React + Next.js，打开就能连接。"),
      description: t(
        "Next.js App Router 构建官网、文档与 Web 控制台，OpenNext 将完整体验部署到 Cloudflare。浏览器直接使用标准 WebRTC 与 Web Crypto。",
      ),
      tags: ["Next.js + React", "OpenNext", "Fumadocs + MDX"],
      points: [
        [
          t("文档也是代码"),
          t(
            "Fumadocs 从 Markdown／MDX 生成目录、搜索和页面，文档与开源核心一起维护。",
          ),
        ],
        [
          t("标准 Web 能力"),
          t(
            "IndexedDB 缓存、虚拟消息列表与响应式布局，让手机、平板和桌面浏览器拥有一致的工作体验。",
          ),
        ],
      ],
      url: "https://fumadocs.dev",
      label: t("了解 Fumadocs"),
    },
    {
      id: "network",
      icon: Network,
      kicker: "03 / CONNECTIVITY",
      title: t("WebRTC + TCP，尽可能直连。"),
      description: t(
        "原生端通过 Pion WebRTC 尝试 UDP 数据通道，同时尝试原生 TCP 打洞；浏览器使用标准 RTCPeerConnection。公共与可选自建 STUN 并行收集候选。",
      ),
      tags: ["WebRTC DataChannel", t("原生 TCP"), "WSS fallback"],
      points: [
        [
          t("一个地址，发现能力"),
          t(
            "客户端只填服务根地址，探测地址从 /p2p/config 下发。可选 Go 探测器在同一 26330 端口监听 TCP 和 UDP。",
          ),
        ],
        [
          t("灵活的连接路径"),
          t(
            "设备间优先建立直连；需要中继时，通过服务提供的加密 WebSocket 路径连接，保持端到端加密。",
          ),
        ],
      ],
      url: "/docs/guides/connections",
      label: t("阅读连接策略"),
    },
    {
      id: "cloud",
      icon: Cloud,
      kicker: "04 / CONNECTION SERVICE",
      title: t("一个 Worker，协调所有连接。"),
      description: t(
        "Cloudflare Worker 承载网站、Web 客户端和 /p2p/*。Durable Objects 为设备房间提供有状态协调与可休眠 WebSocket，D1 持久化设备与配对元数据。",
      ),
      tags: ["Cloudflare Workers", "Durable Objects", "D1"],
      points: [
        [
          t("少一点运维拼装"),
          t(
            "默认 workers.dev 提供 HTTPS；自建无需准备 OAuth、支付或一台必需的公网 VM。探测器按网络需要添加。",
          ),
        ],
        [
          t("一站式连接能力"),
          t(
            "设备发现、配对信令与加密中继共用一个入口。官方托管与开源自建使用相同的客户端和协议。",
          ),
        ],
      ],
      url: "/docs/self-hosting",
      label: t("部署自己的 Worker"),
    },
    {
      id: "data",
      icon: Database,
      kicker: "05 / LOCAL-FIRST STORAGE",
      title: t("本地有缓存，云端有边界。"),
      description: t(
        "原生端使用 SQLite 与附件文件，浏览器使用 IndexedDB 保存配对身份和会话快照。断线恢复时补齐状态，再继续增量事件。",
      ),
      tags: ["SQLite", "IndexedDB", "D1 + Drizzle"],
      points: [
        [
          t("项目留在你的电脑"),
          t(
            "云端不保存聊天与附件明文；原始项目和 Agent 继续运行在被控电脑上。",
          ),
        ],
        [
          t("数据库各司其职"),
          t(
            "SQLite 为原生端保存本地状态，IndexedDB 服务浏览器，D1 持久化云端元数据；官方服务通过 Drizzle ORM 管理业务数据。",
          ),
        ],
      ],
      url: "/docs/architecture/stack",
      label: t("查看完整技术说明"),
    },
    {
      id: "security",
      icon: ShieldCheck,
      kicker: "06 / TRUST & SECURITY",
      title: t("密钥在设备上，授权有边界。"),
      description: t(
        "配对通过 X25519／HKDF／AES-GCM 建立信任，业务通信使用独立方向的 AEAD 密钥并防重放。经中继的消息仍为端到端密文。",
      ),
      tags: [t("端到端加密"), t("本地设备信任"), t("短期签名权益")],
      points: [
        [
          t("编号不是凭据"),
          t(
            "六位设备编号用于发现；首次访问需要被控方确认或验证访问密码。登录同步设备目录，不会替新控制端自动建立配对信任。",
          ),
        ],
        [
          t("统一的服务授权"),
          t(
            "服务签发短期授权，由被控设备验证；直连与中继共享一致的访问控制，设备信任由用户掌握。",
          ),
        ],
      ],
      url: "/docs/privacy",
      label: t("安全与隐私边界"),
    },
  ];
  return (
    <SiteShell>
      <main id="main">
        <PageIntro
          eyebrow="ENGINEERED TO STAY OUT OF YOUR WAY"
          title={
            <>
              {t("原生的轻量，")}
              <br />
              <span>{t("开放的连接。")}</span>
            </>
          }
        >
          {t(
            "原生视图与 Go 核心，标准 Web 与全球边缘网络。从界面到连接，每一层都为轻量、安全与跨平台协作而设计。",
          )}
        </PageIntro>
        <section className="site-width" aria-label={t("架构概览")}>
          <ArchitectureDiagram locale={lang} />
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
            eyebrow="LIGHTWEIGHT BY DESIGN"
            title={t("轻装上阵，把资源留给 Agent。")}
          >
            {t("原生视图与 Go 核心，一个轻量应用，承载完整的跨设备会话体验。")}
          </SectionHeading>
          <div className="performance-grid">
            <div>
              <strong>
                8.9 <span>MB</span>
              </strong>
              <p>{t("macOS arm64 压缩下载体积")}</p>
            </div>
            <div>
              <strong>
                23.1 <span>MB</span>
              </strong>
              <p>{t("macOS .app 文件总大小")}</p>
            </div>
            <div>
              <strong>
                39–48
                <span>MiB</span>
              </strong>
              <p>{t("演示面板／会话物理占用")}</p>
            </div>
          </div>
          <p className="measurement-note">
            {t(
              "Apple M5 · macOS 26.6 · arm64 预览构建 · 演示面板与会话场景，Reign 应用进程物理内存实测。",
            )}
          </p>
          <Link
            className="site-text-link"
            href="/docs/architecture/performance"
          >
            {t("查看测试方法与结果")}
            <ArrowRight size={15} />
          </Link>
        </section>
        <BottomCTA />
      </main>
    </SiteShell>
  );
}
export async function generateMetadata({
  params,
}: {
  params: Promise<{ lang: string }>;
}) {
  const { lang } = await params;
  if (!isLocale(lang)) notFound();
  const [title, description] = marketingMeta["/technology"][lang];
  return pageMetadata(lang, "/technology", title, description);
}
