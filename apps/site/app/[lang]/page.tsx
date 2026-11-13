import { pageMetadata, marketingMeta, siteOrigin } from "../../lib/seo";
import { translate, isLocale } from "../../lib/i18n";
import { notFound } from "next/navigation";
import { LocalizedLink as Link } from "../../components/locale";
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  Cloud,
  Code2,
  Fingerprint,
  FolderOpen,
  Globe,
  HardDrive,
  Monitor,
  Network,
  ShieldCheck,
  Smartphone,
  Terminal,
  Zap,
} from "lucide-react";
import { BottomCTA, SectionHeading, SiteShell } from "../../components/site";
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
                name: "Reign",
                url: `${siteOrigin}/${lang}`,
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
              {t("Agent 在电脑上，")}
              <br />
              <span>{t("掌控在你手中。")}</span>
            </h1>
            <p>
              {t("手机、桌面、浏览器，一个工作空间。")}
              <br />
              {t("连接自己的电脑，继续与你的 AI Agent 对话。")}
            </p>
            <div className="site-actions">
              <Link className="button primary" href="/download">
                {t("下载 Reign")}
                <ArrowUpRight size={16} />
              </Link>
              <Link className="button" href="/chat">
                {t("在浏览器打开")}
                <ArrowRight size={16} />
              </Link>
            </div>
            <div className="hero-platforms">
              <Monitor size={14} /> macOS · Windows · Linux <span />
              <Smartphone size={14} /> Android · iOS <span />
              <Globe size={14} /> Web
            </div>
            <small className="hero-preview-note">
              {t("开发预览 · 平台分发与签名状态见下载页")}
            </small>
          </div>
          <ProductPreview />
        </section>
        <section className="home-facts site-width" aria-label={t("产品要点")}>
          <div>
            <strong>
              8.9 <span>MB</span>
            </strong>
            <p>
              {t("macOS arm64 预览压缩包")}{" "}
              <Link href="/docs/architecture/performance">
                {t("测试条件 ↗")}
              </Link>
            </p>
          </div>
          <div>
            <strong>
              {t("原生")}
              <span>UI</span>
            </strong>
            <p>{t("Go + NSGo，无内嵌 WebView")}</p>
          </div>
          <div>
            <strong>
              {t("直连")}
              <span>{t("优先")}</span>
            </strong>
            <p>{t("可用时 P2P，按服务权益回退中继")}</p>
          </div>
          <div>
            <strong>
              {t("你的")}
              <span>{t("设备")}</span>
            </strong>
            <p>{t("Agent 和工作目录留在本机")}</p>
          </div>
        </section>
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
                  "原生端尝试 UDP 与 TCP 路径，浏览器使用 WebRTC。直连不可用时，按服务权限回退加密中继。",
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
                  "目前接入 Codex App 与 CLI。通过 Provider 扩展更多 Agent，共用同一套连接和会话体验。",
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
              title={t("连接 Agent，而非另造一个 Agent。")}
            >
              {t(
                "使用你已有的模型账号、项目与开发环境。Reign 负责把它们带到你面前。",
              )}
            </SectionHeading>
            <Link className="site-text-link" href="/docs/agents">
              {t("查看接入要求")}
              <ArrowRight size={15} />
            </Link>
          </div>
          <div className="agent-stack">
            <div className="agent-row">
              <Terminal size={25} />
              <div>
                <strong>Codex App</strong>
                <p>{t("通过 Reign 启动，共用 app-server")}</p>
              </div>
              <span className="site-tag available">{t("已接入")}</span>
            </div>
            <div className="agent-row">
              <Code2 size={25} />
              <div>
                <strong>Codex CLI</strong>
                <p>{t("连接兼容后端，管理 CLI 会话")}</p>
              </div>
              <span className="site-tag available">{t("已接入")}</span>
            </div>
            <div className="agent-row planned">
              <PlusIcon />
              <div>
                <strong>{t("更多 Agent")}</strong>
                <p>{t("Claude Code、Pi、ZCode 等待接入")}</p>
              </div>
              <span className="site-tag">{t("待扩展")}</span>
            </div>
            <small>
              {t("已安装不等于可控，具体以 Provider 就绪状态为准。")}
            </small>
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
                t("在电脑打开 Reign"),
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
        <section className="site-width native-section">
          <div>
            <span className="site-eyebrow">SMALL BY DESIGN</span>
            <h2>{t("轻量，不靠省掉体验。")}</h2>
            <p>
              {t(
                "桌面和手机使用 NSGo 原生视图，Go 在同一应用进程中承载核心逻辑。浏览器走标准 WebRTC，不必先下载原生客户端或 Go WASM。",
              )}
            </p>
            <div className="native-points">
              <span>
                <Check size={15} />
                {t("不打包浏览器运行时")}
              </span>
              <span>
                <Check size={15} />
                {t("本地缓存与虚拟消息列表")}
              </span>
              <span>
                <Check size={15} />
                {t("不另起 Hub 后台 CLI")}
              </span>
            </div>
            <Link className="site-text-link" href="/technology">
              {t("看看我们如何构建")}
              <ArrowRight size={15} />
            </Link>
          </div>
          <div className="stack-tiles">
            <div>
              <Code2 />
              <strong>Go + NSGo</strong>
              <small>{t("原生应用")}</small>
            </div>
            <div>
              <Globe />
              <strong>React + Next.js</strong>
              <small>{t("官网与 Web 控制台")}</small>
            </div>
            <div>
              <Cloud />
              <strong>Cloudflare</strong>
              <small>{t("连接服务")}</small>
            </div>
            <div>
              <Fingerprint />
              <strong>E2E encryption</strong>
              <small>{t("端到端加密")}</small>
            </div>
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
            <small>
              {t(
                "自建无需商业订阅；Cloudflare 资源仍按自己的账号配额与计费执行。",
              )}
            </small>
          </div>
          <div className="selfhost-code">
            <div>
              <span className="online-dot" /> YOUR OWN CONNECTION SERVICE
            </div>
            <code>https://your-reign.workers.dev</code>
            <p>{t("一个服务地址，所有客户端。")}</p>
            <span>{t("官网 / 文档 / Web 控制台 / 信令 / 加密中继")}</span>
          </div>
        </section>
        <BottomCTA />
      </main>
    </SiteShell>
  );
}
function PlusIcon() {
  return <span className="agent-plus">+</span>;
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
