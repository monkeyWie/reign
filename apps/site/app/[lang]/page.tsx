import { pageMetadata, marketingMeta, siteOrigin } from "../../lib/seo";
import { translate, isLocale, localizeHref } from "../../lib/i18n";
import { notFound } from "next/navigation";
import { LocalizedLink as Link } from "../../components/locale";
import {
  ArrowRight,
  ArrowUpRight,
  Download,
  FolderGit2,
  MessagesSquare,
  Network,
  Terminal,
  Cpu,
  PackageOpen,
  Server,
  Cloud,
  Monitor,
  Smartphone,
} from "lucide-react";
import { SiteShell } from "../../components/site";
import { PlatformIcon } from "../../components/platform-icon";
import brandPaths from "../../../../internal/brandicons/paths.json";
import "./home.css";
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
        <section className="access-hero site-width">
          <div className="access-intro">
            <h1>
              {t("你的 Agent")}
              <br />
              <em>{t("聚合工作台。")}</em>
            </h1>
            <p>
              {t(
                "把 Codex、Claude Code、Pi 的会话放进同一个项目视图。在电脑上安排任务，在手机或浏览器里接着做。",
              )}
            </p>
            <div className="site-actions">
              <Link className="button primary" href="/download">
                <Download size={16} />
                {t("下载 ReignHub")}
              </Link>
              <Link className="button" href="/chat">
                {t("在浏览器打开")}
                <ArrowUpRight size={16} />
              </Link>
            </div>
          </div>
          <figure className="session-scene">
            <figcaption>{t("工作台示例")}</figcaption>
            <div className="session-sheet">
              <div className="sheet-bar">
                <span>
                  <Monitor size={15} /> MacBook Pro
                </span>
                <span className="sheet-path">~/atlas</span>
              </div>
              <strong className="sheet-title">atlas</strong>
              <small>{t("项目会话")}</small>
              <ul className="sheet-sessions">
                {(
                  [
                    ["openai", "Codex", t("搜索栏的键盘导航")],
                    ["claude", "Claude Code", t("复核登录流程")],
                    ["pi", "Pi", t("整理发布清单")],
                  ] as const
                ).map(([brand, agent, title]) => (
                  <li key={brand}>
                    <svg viewBox="0 0 24 24" aria-hidden="true">
                      <path fill="currentColor" d={brandPaths[brand]} />
                    </svg>
                    <span>{title}</span>
                    <small>{agent}</small>
                  </li>
                ))}
              </ul>
            </div>
            <div className="session-followup">
              <span>
                <Smartphone size={15} />
                {t("来自手机")}
                <time>14:32</time>
              </span>
              <p>{t("再测一下没有结果的情况。")}</p>
            </div>
          </figure>
        </section>
        <nav
          className="access-platforms site-width"
          aria-label={t("下载客户端")}
        >
          {(
            [
              ["macos", "macOS"],
              ["windows", "Windows"],
              ["linux", "Linux"],
              ["ios", "iOS"],
              ["android", "Android"],
              ["web", "Web"],
            ] as const
          ).map(([id, label]) => (
            <Link key={id} href={id === "web" ? "/chat" : `/download#${id}`}>
              <PlatformIcon platform={id} size={19} />
              {label}
            </Link>
          ))}
        </nav>
        <section className="access-work site-width">
          <h2>{t("从项目到终端。")}</h2>
          <div className="access-steps">
            {[
              {
                Icon: FolderGit2,
                title: t("打开你的项目"),
                body: t(
                  "按工作目录找到 Codex、Pi 或 ACP Agent 的会话。代码和模型凭证留在运行 Agent 的电脑上。",
                ),
                href: "/docs/agents",
              },
              {
                Icon: MessagesSquare,
                title: t("看进度，回消息"),
                body: t(
                  "在另一台设备上读最新回复、查看文件，或给 Agent 发下一条指令。",
                ),
                href: "/docs/guides/sessions",
              },
              {
                Icon: Terminal,
                title: t("远程终端"),
                body: t(
                  "从桌面会话打开项目目录里的 Shell。切换终端标签或会话后，命令会继续在原电脑运行。",
                ),
                href: "/docs/guides/terminal",
              },
              {
                Icon: Network,
                title: t("P2P 直连"),
                body: t(
                  "设备优先尝试直连，按网络条件选择可用路径。通过中继连接时，会话与附件同样端到端加密。",
                ),
                href: "/docs/guides/connections",
              },
              {
                Icon: PackageOpen,
                title: t("原生 UI 与包体积"),
                body: t(
                  "桌面和手机使用 NSGo 原生界面。下载页列出各平台安装包的实际大小，浏览器可直接打开 Web 工作台。",
                ),
                href: "/download",
              },
              {
                Icon: Cpu,
                title: t("按需使用内存"),
                body: t(
                  "打开会话时先读取近期消息，滚动时再加载相邻内容。查看 macOS 上的测量场景、内存占用和采样方法。",
                ),
                href: "/docs/architecture/performance",
              },
            ].map(({ Icon, title, body, href }) => (
              <Link href={href} key={href}>
                <Icon size={23} />
                <h3>
                  {title}
                  <ArrowUpRight size={16} />
                </h3>
                <p>{body}</p>
              </Link>
            ))}
          </div>
        </section>
        <section className="access-host site-width">
          <div>
            <h2>{t("部署自己的连接服务。")}</h2>
            <p>
              {t(
                "用 Go 服务或 Cloudflare Worker 配对设备、交换连接信息，并在需要时转发加密流量。",
              )}
            </p>
            <Link className="site-text-link" href="/docs/self-hosting">
              {t("开源自部署指南")}
              <ArrowRight size={16} />
            </Link>
          </div>
          <div className="access-host-links">
            <Link href="/docs/self-hosting/go">
              <Server />
              <strong>{t("独立 Go 服务")}</strong>
              <small>Linux · macOS · Windows</small>
              <ArrowUpRight />
            </Link>
            <Link href="/docs/self-hosting/cloudflare">
              <Cloud />
              <strong>Cloudflare Worker</strong>
              <small>D1 · Durable Objects</small>
              <ArrowUpRight />
            </Link>
          </div>
        </section>
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
