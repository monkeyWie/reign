import { PlatformIcon } from "../../components/platform-icon";
import { pageMetadata, marketingMeta, siteOrigin } from "../../lib/seo";
import { translate, isLocale, localizeHref } from "../../lib/i18n";
import { notFound } from "next/navigation";
import { LocalizedLink as Link } from "../../components/locale";
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  Layers3,
  Code2,
  FolderOpen,
  Globe,
  HardDrive,
  Network,
  ShieldCheck,
  Terminal,
  Zap,
} from "lucide-react";
import { BottomCTA, SectionHeading, SiteShell } from "../../components/site";
import { PlatformShowcase } from "../../components/platform-showcase";
import { ProductPreview } from "../../components/product-preview";

export default async function Home({
  params,
}: {
  params: Promise<{ lang: string }>;
}) {
  const { lang } = await params;
  if (!isLocale(lang)) notFound();
  const t = translate(lang);
  return (
    <SiteShell>
      <main id="main">
        {siteOrigin && (
          <script
            type="application/ld+json"
            dangerouslySetInnerHTML={{
              __html: JSON.stringify({
                "@context": "https://schema.org",
                "@type": "WebSite",
                name: "ReignHub",
                url: siteOrigin + localizeHref("/", lang),
                description: marketingMeta["/"][lang][1],
                inLanguage: lang,
              }).replace(/</g, "\\u003c"),
            }}
          />
        )}
        <section className="home-hero site-width">
          <div className="hero-copy">
            <span className="site-eyebrow">
              <span className="online-dot" /> YOUR AGENTS. ANYWHERE.
            </span>
            <h1>
              {t("掌控你的 Agent，")}
              <br />
              <span>{t("不止于桌面。")}</span>
            </h1>
            <p>
              {t("六大平台，一个 Agent 工作空间。")}
              <br />
              {t("让电脑持续创造，让你随处掌控。原生、轻量、端到端加密。")}
            </p>
            <div className="site-actions">
              <Link className="button primary" href="/download">
                {t("下载 ReignHub")}
                <ArrowUpRight size={16} />
              </Link>
              <Link className="button" href="/chat">
                {t("在浏览器打开")}
                <ArrowRight size={16} />
              </Link>
            </div>
            <div className="hero-platforms">
              {(
                ["macos", "windows", "linux", "android", "ios", "web"] as const
              ).map((platform, index) => (
                <span key={platform} className="hero-platform-item">
                  <PlatformIcon platform={platform} size={14} />
                  {
                    ["macOS", "Windows", "Linux", "Android", "iOS", "Web"][
                      index
                    ]
                  }
                </span>
              ))}
            </div>
          </div>
          <ProductPreview />
        </section>
        <section className="home-facts site-width" aria-label={t("产品要点")}>
          <div>
            <strong>
              6<span>{t("大平台")}</span>
            </strong>
            <p>{t("桌面、手机、浏览器，全面连接")}</p>
          </div>
          <div>
            <strong>
              8.9<span>MB</span>
            </strong>
            <p>{t("macOS arm64 压缩包")}</p>
          </div>
          <div>
            <strong>
              39–48<span>MiB</span>
            </strong>
            <p>{t("macOS 演示场景内存实测")}</p>
          </div>
          <div>
            <strong>
              E2E<span>{t("加密")}</span>
            </strong>
            <p>{t("会话与附件，只在你的设备解密")}</p>
          </div>
        </section>
        <PlatformShowcase lang={lang} />
        <section className="site-section site-width">
          <SectionHeading
            eyebrow="ONE WORKSPACE"
            title={t("你的 Agent 工作，不再被一块屏幕限制。")}
          >
            {t("从设备连接到最后一条回复，把远程协作的每一步放在一起。")}
          </SectionHeading>
          <div className="site-feature-grid">
            {[
              [
                FolderOpen,
                t("会话按项目，各就各位"),
                t(
                  "聚合设备上的会话，按工作目录分组、最近更新排序。切换设备，继续原来的上下文。",
                ),
              ],
              [
                Network,
                t("尽可能直接，连接彼此"),
                t(
                  "UDP、TCP 与 WebRTC 多路径探索，优先建立设备间直连。端到端加密贯穿直连与中继。",
                ),
              ],
              [
                ShieldCheck,
                t("先授权，再开始"),
                t(
                  "首次连接需要电脑确认或访问密码。会话和附件端到端加密，配对密钥保留在设备本地。",
                ),
              ],
              [
                Terminal,
                t("真实 Agent，真实会话"),
                t(
                  "接入 Codex、Claude Code、Pi 与 ACP Agent，统一管理项目、会话与实时消息。",
                ),
              ],
              [
                Zap,
                t("输出发生时，你就能看到"),
                t(
                  "增量文本、Markdown、折叠的运行活动、模型选择。长会话由虚拟列表承载。",
                ),
              ],
              [
                HardDrive,
                t("断线以后，仍有脉络"),
                t(
                  "原生 SQLite、浏览器 IndexedDB 缓存会话与附件。重新上线补同步，继续接收新消息。",
                ),
              ],
            ].map(([Icon, title, description]) => {
              const I = Icon as typeof FolderOpen;
              return (
                <article className="site-feature" key={String(title)}>
                  <span className="feature-icon">
                    <I size={21} />
                  </span>
                  <h3>{String(title)}</h3>
                  <p>{String(description)}</p>
                </article>
              );
            })}
          </div>
          <Link className="site-text-link" href="/features">
            {t("了解全部特性")}
            <ArrowRight size={15} />
          </Link>
        </section>
        <section className="site-section site-width agents-section">
          <div>
            <SectionHeading
              eyebrow="BUILT FOR YOUR AGENTS"
              title={t("熟悉的 Agent，更大的掌控半径。")}
            >
              {t(
                "使用你已有的模型账号、项目与开发环境。ReignHub 负责把它们带到你面前。",
              )}
            </SectionHeading>
            <Link className="site-text-link" href="/docs/agents">
              {t("连接你的 Agent")}
              <ArrowRight size={15} />
            </Link>
          </div>
          <div className="agent-stack">
            <div className="agent-row">
              <Terminal size={25} />
              <div>
                <strong>Codex App</strong>
                <p>{t("桌面与远端，共享同一段对话")}</p>
              </div>
              <span className="site-tag available">{t("已接入")}</span>
            </div>
            <div className="agent-row">
              <Code2 size={25} />
              <div>
                <strong>Codex CLI</strong>
                <p>{t("从终端到手机，项目上下文随行")}</p>
              </div>
              <span className="site-tag available">{t("已接入")}</span>
            </div>
            {[
              ["Claude Code", t("成熟 Agent，统一远程体验")],
              ["Pi", t("原生 RPC，实时消息与工具审批")],
              ["ACP", "Cursor · OpenCode · Gemini · DeepSeek Harness"],
            ].map(([name, detail]) => (
              <div className="agent-row" key={name}>
                <Layers3 size={25} />
                <div>
                  <strong>{name}</strong>
                  <p>{detail}</p>
                </div>
                <span className="site-tag available">{t("已接入")}</span>
              </div>
            ))}
            <div className="agent-architecture">
              <Layers3 size={18} />
              <p>
                {t(
                  "可扩展 Provider 架构，让不同 Agent 共享连接、会话与安全能力。",
                )}
              </p>
            </div>
          </div>
        </section>
        <section className="site-section site-width">
          <SectionHeading
            eyebrow="THREE STEPS"
            title={t("从桌面到口袋，只需一次连接。")}
          />
          <div className="start-steps">
            {[
              [
                "01",
                t("在电脑打开 ReignHub"),
                t("确认 Agent 已安装，并按照面板提示完成接入。"),
              ],
              [
                "02",
                t("输入编号，或手机扫码"),
                t("由电脑确认授权，或使用预先设置的访问密码。"),
              ],
              [
                "03",
                t("打开项目，继续对话"),
                t("在手机、另一台电脑或浏览器查看并操作会话。"),
              ],
            ].map(([n, title, description]) => (
              <div key={n}>
                <span>{n}</span>
                <h3>{title}</h3>
                <p>{description}</p>
              </div>
            ))}
          </div>
          <Link href="/docs/quick-start" className="site-text-link">
            {t("阅读快速开始")}
            <ArrowRight size={15} />
          </Link>
        </section>
        <section className="site-width native-section performance-spotlight">
          <div>
            <span className="site-eyebrow">NATIVE. LIGHT. POWERFUL.</span>
            <h2>{t("把性能留给 Agent，把自由交给你。")}</h2>
            <p>
              {t(
                "原生视图，Go 驱动。不打包浏览器运行时，以轻巧的体积承载完整的远程工作体验。",
              )}
            </p>
            <div className="native-points">
              <span>
                <Check size={15} />
                {t("8.9 MB macOS 压缩包，原生轻巧")}
              </span>
              <span>
                <Check size={15} />
                {t("低内存常驻，随时准备接入")}
              </span>
              <span>
                <Check size={15} />
                {t("流式对话、图片附件、长会话虚拟滚动")}
              </span>
            </div>
            <Link className="site-text-link" href="/technology">
              {t("探索轻量背后的技术")}
              <ArrowRight size={15} />
            </Link>
          </div>
          <div className="performance-card">
            <span className="site-eyebrow">
              SMALL FOOTPRINT. FULL EXPERIENCE.
            </span>
            <div className="performance-number">
              48.4 <span>MiB</span>
            </div>
            <p>{t("打开对话，依然轻盈。")}</p>
            <div className="memory-sample">
              <div>
                <span>{t("待机面板")}</span>
                <strong>39.5 MiB</strong>
              </div>
              <div className="memory-track">
                <i style={{ width: "65.3%" }} />
              </div>
            </div>
            <div className="memory-sample">
              <div>
                <span>{t("Markdown 丰富对话")}</span>
                <strong>48.4 MiB</strong>
              </div>
              <div className="memory-track">
                <i style={{ width: "80%" }} />
              </div>
            </div>
            <small>
              {t("Apple M5 · macOS · ReignHub 应用进程实测")}{" "}
              <Link href="/docs/architecture/performance">
                {t("查看测试 ↗")}
              </Link>
            </small>
          </div>
        </section>
        <section className="site-section site-width self-host-section">
          <div>
            <span className="site-eyebrow">OPEN AT THE CORE</span>
            <h2>{t("官方服务，或者你自己的服务。")}</h2>
            <p>
              {t(
                "官网、文档、客户端和连接核心一起开放。自建一个 Worker，获得自己的设备发现、Web 控制台和加密中继。",
              )}
            </p>
            <div className="site-actions">
              <Link href="/docs/self-hosting" className="button primary">
                {t("部署自己的服务")}
                <ArrowUpRight size={15} />
              </Link>
              <Link href="/pricing" className="button">
                {t("比较使用方式")}
              </Link>
            </div>
          </div>
          <div className="selfhost-code">
            <div>
              <span className="online-dot" /> YOUR OWN CONNECTION SERVICE
            </div>
            <code>https://your-reignhub.workers.dev</code>
            <p>{t("一个服务地址，所有客户端。")}</p>
            <span>{t("官网 / 文档 / Web 控制台 / 信令 / 加密中继")}</span>
          </div>
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
  const [title, description] = marketingMeta["/"][lang];
  return pageMetadata(lang, "/", title, description);
}
