"use client";
import { useT, useLocale } from "./locale";
import { LocalizedLink as Link } from "./locale";
import { localizeHref, switchLocale } from "../lib/i18n";
import NextLink from "next/link";
import { usePathname } from "next/navigation";
import { ArrowUpRight, Menu } from "lucide-react";

export function SiteHeader() {
  const t = useT();
  const locale = useLocale();
  const pathname = usePathname();
  const links = [
    ["/features", t("特性")],
    ["/technology", t("技术栈")],
    ["/docs", t("文档")],
    ["/pricing", t("价格")],
  ];
  return (
    <header className="site-header">
      <div className="site-header-inner">
        <Link href="/" className="brand" aria-label={t("Reign 首页")}>
          <span className="brand-symbol">♜</span> Reign
        </Link>
        <nav className="site-nav" aria-label={t("主导航")}>
          {links.map(([href, title]) => (
            <Link
              key={href}
              href={href}
              aria-current={
                pathname === localizeHref(href, locale) ? "page" : undefined
              }
            >
              {title}
            </Link>
          ))}
        </nav>
        <div className="site-header-actions">
          <NextLink
            className="site-language-link"
            href={switchLocale(pathname, locale === "en" ? "zh-CN" : "en")}
            hrefLang={locale === "en" ? "zh-CN" : "en"}
            lang={locale === "en" ? "zh-CN" : "en"}
            aria-label={
              locale === "en"
                ? "Switch to Simplified Chinese"
                : "Switch to English"
            }
          >
            {locale === "en" ? "简体中文" : "English"}
          </NextLink>
          <Link className="site-console-link" href="/chat">
            {t("控制台")}
            <ArrowUpRight size={14} />
          </Link>
          <Link className="button primary site-download-link" href="/download">
            {t("下载客户端")}
          </Link>
        </div>
        <details className="site-mobile-menu" key={pathname}>
          <summary aria-label={t("展开导航")}>
            <Menu size={22} />
          </summary>
          <nav aria-label={t("移动导航")}>
            {links.map(([href, title]) => (
              <Link
                key={href}
                href={href}
                aria-current={
                  pathname === localizeHref(href, locale) ? "page" : undefined
                }
              >
                {title}
              </Link>
            ))}
            <Link href="/download">{t("下载客户端")}</Link>
            <Link href="/chat">{t("打开控制台 ↗")}</Link>
          </nav>
        </details>
      </div>
    </header>
  );
}
