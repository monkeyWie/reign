"use client";

import { useEffect, useState } from "react";
import {
  ArrowUpRight,
  Code2,
  Download,
  Laptop,
  Monitor,
  Smartphone,
  Terminal,
} from "lucide-react";
import { translate, type Locale } from "../lib/i18n";
import {
  releasesURL,
  type Platform,
  type ReleaseCatalog,
} from "../lib/releases";
import { LocalizedLink as Link } from "./locale";

export function ReleaseDownloads({ lang }: { lang: Locale }) {
  const t = translate(lang);
  const [catalog, setCatalog] = useState<ReleaseCatalog | null>(null);
  const [loading, setLoading] = useState(true);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const abort = new AbortController();
    setLoading(true);
    fetch("/api/releases", { signal: abort.signal })
      .then(async (response) => {
        if (!response.ok) throw new Error("release catalogue unavailable");
        const value: ReleaseCatalog = await response.json();
        if (!abort.signal.aborted) setCatalog(value);
      })
      .catch(() => {
        /* Keep an already loaded list when a refresh fails. */
      })
      .finally(() => {
        if (!abort.signal.aborted) setLoading(false);
      });
    return () => abort.abort();
  }, [retry]);
  const release = catalog?.release;
  const platforms = [
    {
      id: "macos",
      Icon: Monitor,
      title: "macOS",
      arch: "Apple Silicon / Intel",
      format: t(".app 归档"),
      note: t(
        "当前预览为 ad hoc 签名，尚未完成 Apple 公证。请在可信来源获取。",
      ),
    },
    {
      id: "windows",
      Icon: Laptop,
      title: "Windows",
      arch: "x64 / arm64",
      format: t("便携 ZIP"),
      note: t("提供 x64 与 arm64 原生构建，解压后启动。"),
    },
    {
      id: "linux",
      Icon: Terminal,
      title: "Linux",
      arch: "x64 / arm64",
      format: "tar.gz",
      note: t("采用 GTK 4 原生界面。安装 GTK 4 运行库后，解压并启动。"),
    },
    {
      id: "android",
      Icon: Smartphone,
      title: "Android",
      arch: "arm64",
      format: "APK",
      note: t("下载并安装 APK，随时连接你的 Agent。"),
    },
    {
      id: "ios",
      Icon: Smartphone,
      title: "iOS",
      arch: "iPhone / iPad",
      format: t("未签名 IPA"),
      note: t("适用于 iPhone 与 iPad，真机安装需使用自己的 Apple 开发签名。"),
    },
  ] satisfies {
    id: Platform;
    Icon: typeof Monitor;
    title: string;
    arch: string;
    format: string;
    note: string;
  }[];
  return (
    <>
      <section
        className="site-width release-notice"
        aria-live="polite"
        aria-busy={loading}
      >
        <span className="site-tag">
          {release
            ? release.prerelease
              ? t("开发预览")
              : t("最新版本")
            : "REIGN"}
        </span>
        <div>
          <h2>
            {release
              ? `Reign ${release.version}`
              : loading
                ? t("正在获取最新版本…")
                : t("获取 Reign")}
          </h2>
          <p>
            {release
              ? `${t("发布于")} ${new Intl.DateTimeFormat(lang, { dateStyle: "medium", timeZone: "UTC" }).format(new Date(release.publishedAt))}`
              : loading
                ? t("正在加载各平台安装包。")
                : catalog?.checkedAt
                  ? t("安装包发布后会自动显示在这里。")
                  : t("暂时无法获取版本，请在 GitHub Releases 查看下载。")}
          </p>
          <div className="site-actions">
            <a href={release?.url || releasesURL} className="site-text-link">
              {t("查看 Releases ↗")}
            </a>
            {release?.checksumsURL && (
              <a href={release.checksumsURL} className="site-text-link">
                SHA256SUMS <ArrowUpRight size={14} />
              </a>
            )}
            {!loading && !release && (
              <button
                type="button"
                className="button"
                onClick={() => setRetry((value) => value + 1)}
              >
                {t("重新加载")}
              </button>
            )}
          </div>
        </div>
      </section>
      <section className="site-section site-width">
        <div className="download-grid">
          {platforms.map(({ id, Icon, title, arch, format, note }) => {
            const assets =
              release?.assets
                .filter((a) => a.platform === id)
                .sort((a, b) => a.architecture.localeCompare(b.architecture)) ||
              [];
            return (
              <article key={id}>
                <Icon size={25} />
                <h2>{title}</h2>
                <p className="download-arch">{arch}</p>
                <span className="site-tag">{format}</span>
                <p>{note}</p>
                <div className="download-assets">
                  {assets.map((asset) => (
                    <a
                      key={asset.name}
                      href={asset.url}
                      className="button"
                      title={asset.name}
                    >
                      <Download size={14} />
                      {id === "macos"
                        ? asset.architecture === "arm64"
                          ? "Apple Silicon"
                          : "Intel"
                        : id === "ios"
                          ? "IPA"
                          : asset.architecture}
                      <span className="download-size">
                        {(asset.size / 1024 / 1024).toFixed(1)} MB
                      </span>
                    </a>
                  ))}
                  {!assets.length && (
                    <span className="download-unavailable">
                      {loading ? t("正在加载…") : t("安装包即将发布")}
                    </span>
                  )}
                </div>
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
    </>
  );
}
