import { pageMetadata, marketingMeta } from "../../../lib/seo";
import { translate, isLocale } from "../../../lib/i18n";
import { notFound } from "next/navigation";
import { LocalizedLink as Link } from "../../../components/locale";
import { ArrowUpRight, Globe } from "lucide-react";
import { ReleaseDownloads } from "../../../components/release-downloads";
import { PageIntro, SiteShell } from "../../../components/site";

export default async function DownloadPage({
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
        <ReleaseDownloads lang={lang} />
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
