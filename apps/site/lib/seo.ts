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
      siteName: "ReignHub",
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
                alt: "ReignHub — Your agents. Anywhere.",
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
      "ReignHub — Your agents. Anywhere.",
      "Your agents, beyond your desk. ReignHub spans macOS, Windows, Linux, iOS, Android and Web. Native UI, an 8.9 MB macOS archive, low memory usage and end-to-end encryption.",
    ],
    "zh-CN": [
      "ReignHub — 你的 Agent，随处掌控",
      "掌控你的 Agent，不止于桌面。ReignHub 覆盖 macOS、Windows、Linux、iOS、Android 与 Web，原生轻量，macOS 包体积 8.9 MB、演示内存实测 39–48 MiB，端到端加密。",
    ],
  },
  "/features": {
    en: [
      "Features · ReignHub",
      "One workspace across six platforms. Manage agent sessions by project with native and Web clients, live output, encrypted files, direct connections and self-hosting.",
    ],
    "zh-CN": [
      "特性 · ReignHub",
      "六大平台，一个 Agent 工作空间。跨设备聚合项目会话、实时输出、图片附件、原生低内存、P2P 直连与端到端加密，支持自建服务。",
    ],
  },
  "/technology": {
    en: [
      "Technology · ReignHub",
      "Explore NSGo native UI, the Go core, React Web, WebRTC/TCP, Cloudflare Workers, D1 and measured package size and memory.",
    ],
    "zh-CN": [
      "技术栈 · ReignHub",
      "了解 NSGo 原生 UI、Go 核心、React Web、WebRTC／TCP、Cloudflare Worker、D1，以及包体积和内存实测。",
    ],
  },
  "/pricing": {
    en: [
      "Pricing · ReignHub",
      "Compare ReignHub official free, paid and open-source self-hosted options, including host device limits, agent products and relay access.",
    ],
    "zh-CN": [
      "价格 · ReignHub",
      "比较 ReignHub 官方免费、付费与开源自建方案，了解设备额度、Agent 产品和中继权益。",
    ],
  },
  "/download": {
    en: [
      "Downloads · ReignHub",
      "Platform support, developer previews and installation notes for ReignHub desktop, Android, iOS and Web clients.",
    ],
    "zh-CN": [
      "下载 · ReignHub",
      "ReignHub 原生桌面、Android、iOS 与 Web 的平台支持、开发预览和安装说明。",
    ],
  },
} as const;
