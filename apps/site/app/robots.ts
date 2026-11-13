import type { MetadataRoute } from "next";
import { siteOrigin } from "../lib/seo";
export default function robots(): MetadataRoute.Robots {
  return {
    rules: {
      userAgent: "*",
      allow: ["/en", "/zh-CN"],
      disallow: ["/chat", "/account", "/admin", "/api/", "/p2p/"],
    },
    sitemap: siteOrigin ? `${siteOrigin}/sitemap.xml` : undefined,
  };
}
