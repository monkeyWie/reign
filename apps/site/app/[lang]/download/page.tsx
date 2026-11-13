import { pageMetadata, marketingMeta } from "../../../lib/seo";
import { translate, isLocale } from "../../../lib/i18n";
import { notFound } from "next/navigation";
import { LocalizedLink as Link } from "../../../components/locale";
import {
  ArrowUpRight,
  Code2,
  Download,
  Globe,
  Laptop,
  Monitor,
  Smartphone,
  Terminal,
} from "lucide-react";
import { PageIntro, SiteShell } from "../../../components/site";

export default async function DownloadPage({
  params,
}: {
  params: Promise<{ lang: string }>;
}) {
  const { lang } = await params;
  if (!isLocale(lang)) notFound();
  const t = translate(lang);
  const platforms = [
    [
      Monitor,
      "macOS",
      "Apple Silicon / Intel",
      t(".app 归档"),
      t("当前预览为 ad hoc 签名，尚未完成 Apple 公证。请在可信来源获取。"),
    ],
    [
      Laptop,
      "Windows",
      "x64 / arm64",
      t("便携 ZIP"),
      t("提供 x64 与 arm64 原生构建，解压后启动。"),
    ],
    [
      Terminal,
      "Linux",
      "x64 / arm64",
      "tar.gz",
      t("采用 GTK 4 原生界面。安装 GTK 4 运行库后，解压并启动。"),
    ],
    [
      Smartphone,
      "Android",
      "arm64",
      "APK",
      t(
        "直接安装预览 APK。后续覆盖安装需要保持相同签名，不能覆盖不同签名的旧包。",
      ),
    ],
    [
      Smartphone,
      "iOS",
      "iPhone / iPad",
      t("未签名 IPA"),
      t("适用于 iPhone 与 iPad，真机安装需使用自己的 Apple 开发签名。"),
    ],
  ];
  return (
    <SiteShell>
      <main id="main">
        <PageIntro
          eyebrow="NATIVE WHEN YOU WANT IT"
          title={
            <>
              {t("选一个屏幕，")}
              <br />
              <span>{t("带走你的工作。")}</span>
            </>
          }
        >
          {t(
            "桌面端负责本机 Agent，也能连接其他电脑。手机和浏览器用于控制，设备之间保留统一的会话体验。",
          )}
        </PageIntro>
        <section className="site-width download-web">
          <div>
            <Globe size={28} />
            <h2>{t("也可以，什么都不安装。")}</h2>
            <p>
              {t(
                "浏览器打开控制台，输入设备编号，配对后继续对话。被控电脑仍需运行 Reign 桌面端。",
              )}
            </p>
          </div>
          <Link className="button primary" href="/chat">
            {t("打开 Web 控制台")}
            <ArrowUpRight size={16} />
          </Link>
        </section>
        <section className="site-width release-notice">
          <span className="site-tag">{t("开发预览")}</span>
          <div>
            <h2>{t("获取最新构建")}</h2>
            <p>
              {t(
                "通过 GitHub Actions 选择对应平台的最新构建，或在 Releases 查看版本发布。安装与源码构建步骤见文档。",
              )}
            </p>
            <div className="site-actions">
              <a
                href="https://github.com/monkeyWie/reign/actions/workflows/ci.yml"
                className="button"
              >
                <Download size={15} />
                {t("查看构建产物")}
              </a>
              <a
                href="https://github.com/monkeyWie/reign/releases"
                className="site-text-link"
              >
                {t("查看 Releases ↗")}
              </a>
            </div>
          </div>
        </section>
        <section className="site-section site-width">
          <div className="download-grid">
            {platforms.map(([Icon, title, arch, format, note]) => {
              const I = Icon as typeof Monitor;
              return (
                <article key={String(title)}>
                  <I size={25} />
                  <h2>{String(title)}</h2>
                  <p className="download-arch">{String(arch)}</p>
                  <span className="site-tag">{String(format)}</span>
                  <p>{String(note)}</p>
                  <a
                    className="site-text-link"
                    href="https://github.com/monkeyWie/reign/actions/workflows/ci.yml"
                  >
                    {t("查看预览构建")}
                    <ArrowUpRight size={14} />
                  </a>
                </article>
              );
            })}
            <article className="download-source">
              <Code2 size={25} />
              <h2>{t("从源码构建")}</h2>
              <p className="download-arch">Go + NSGo · Next.js</p>
              <span className="site-tag">MIT</span>
              <p>
                {t(
                  "使用自己的默认服务地址，或为目标平台构建原生客户端。版本与工具链以仓库为准。",
                )}
              </p>
              <Link
                className="site-text-link"
                href="/docs/contributing/development"
              >
                {t("本地开发指南")}
                <ArrowUpRight size={14} />
              </Link>
            </article>
          </div>
        </section>
        <section className="site-width download-help">
          <h2>{t("第一次使用？")}</h2>
          <p>
            {t(
              "先在安装 Agent 的电脑上启动 Reign，按面板提示接入 Codex。然后在另一台设备输入编号，完成授权。",
            )}
          </p>
          <Link className="button" href="/docs/quick-start">
            {t("查看快速开始")}
            <ArrowUpRight size={15} />
          </Link>
          <p className="measurement-note">
            {t("轻巧原生：macOS 压缩包 8.9 MB，演示场景内存 39–48 MiB。查看")}{" "}
            <Link href="/docs/architecture/performance">{t("性能说明")}</Link>。
          </p>
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
  const [title, description] = marketingMeta["/download"][lang];
  return pageMetadata(lang, "/download", title, description);
}
