import type { Metadata } from "next";
import { localizeHref, locales, type Locale } from "./i18n";

// Set at build time. A self-hosted instance must never canonicalize to the SaaS site.
const configured = process.env.SITE_URL;
export const siteOrigin = configured ? new URL(configured).origin : undefined;
export function languageAlternates(path: string) {
  if (!siteOrigin) return undefined;
  return {
    ...Object.fromEntries(
      locales.map((lang) => [lang, siteOrigin + localizeHref(path, lang)]),
    ),
    "x-default": siteOrigin + localizeHref(path, "en"),
  };
}
export function pageMetadata(
  locale: Locale,
  path: string,
  title: string,
  description: string,
): Metadata {
  const url = siteOrigin && siteOrigin + localizeHref(path, locale);
  const image = siteOrigin && `${siteOrigin}/og/${locale}.png`;
  return {
    title,
    description,
    alternates: siteOrigin
      ? { canonical: url, languages: languageAlternates(path) }
      : undefined,
    openGraph: {
      title,
      description,
      siteName: "Reign",
      type: "website",
      url,
      locale: locale === "en" ? "en_US" : "zh_CN",
      alternateLocale: [locale === "en" ? "zh_CN" : "en_US"],
      ...(image
        ? {
            images: [
              {
                url: image,
                width: 1200,
                height: 630,
                alt: "Reign — Your agents. Anywhere.",
              },
            ],
          }
        : {}),
    },
    twitter: {
      card: "summary_large_image",
      title,
      description,
      ...(image ? { images: [image] } : {}),
    },
  };
}
export const marketingMeta = {
  "/": {
    en: [
      "Reign — Your agents. Anywhere.",
      "Control Codex App and CLI on your computer from native desktop, mobile and Web clients. Direct connections first, end-to-end encrypted and self-hostable.",
    ],
    "zh-CN": [
      "Reign — 你的 Agent，随处掌控",
      "原生桌面与手机客户端，加上 Web 控制台，远控自己电脑上的 Codex App 与 CLI。直连优先、端到端加密，支持自行部署。",
    ],
  },
  "/features": {
    en: [
      "Features · Reign",
      "Manage agent sessions across devices with native and Web clients, direct connections, encrypted attachments and self-hosting.",
    ],
    "zh-CN": [
      "特性 · Reign",
      "跨设备管理 Agent 会话，原生与 Web、直连优先、端到端加密及自行部署。",
    ],
  },
  "/technology": {
    en: [
      "Technology · Reign",
      "Explore NSGo native UI, the Go core, React Web, WebRTC/TCP, Cloudflare Workers, D1 and measured package size and memory.",
    ],
    "zh-CN": [
      "技术栈 · Reign",
      "了解 NSGo 原生 UI、Go 核心、React Web、WebRTC／TCP、Cloudflare Worker、D1，以及包体积和内存实测。",
    ],
  },
  "/pricing": {
    en: [
      "Pricing · Reign",
      "Compare Reign official free, paid and open-source self-hosted options, including host device limits, agent products and relay access.",
    ],
    "zh-CN": [
      "价格 · Reign",
      "比较 Reign 官方免费、付费与开源自建方案，了解设备额度、Agent 产品和中继权益。",
    ],
  },
  "/download": {
    en: [
      "Downloads · Reign",
      "Platform support, developer previews and installation notes for Reign desktop, Android, iOS and Web clients.",
    ],
    "zh-CN": [
      "下载 · Reign",
      "Reign 原生桌面、Android、iOS 与 Web 的平台支持、开发预览和安装说明。",
    ],
  },
} as const;
