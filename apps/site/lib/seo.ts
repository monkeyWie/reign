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
      "ReignHub — One workspace for your agents",
      "Organize Codex, Claude Code and Pi sessions by project. Work locally or remotely with native apps, a Web workspace, P2P connections, desktop terminals and open-source self-hosting.",
    ],
    "zh-CN": [
      "ReignHub — 你的 Agent 聚合工作台",
      "按项目聚合 Codex、Claude Code、Pi 等 Agent 会话。原生客户端与 Web 跨平台访问，支持 P2P 直连、桌面远程终端和开源自托管。",
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
      "Explore NSGo native UI, the Go core, NSGo WebAssembly, WebRTC/TCP, Cloudflare Workers, D1 and measured package size and memory.",
    ],
    "zh-CN": [
      "技术栈 · ReignHub",
      "了解 NSGo 原生 UI、Go 核心、NSGo WebAssembly、WebRTC／TCP、Cloudflare Worker、D1，以及包体积和内存实测。",
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
