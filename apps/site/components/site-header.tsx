"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ArrowUpRight, Menu } from "lucide-react";

const links = [
  ["/features", "特性"],
  ["/technology", "技术栈"],
  ["/docs", "文档"],
  ["/pricing", "价格"],
];
export function SiteHeader() {
  const pathname = usePathname();
  return (
    <header className="site-header">
      <div className="site-header-inner">
        <Link href="/" className="brand" aria-label="Reign 首页">
          <span className="brand-symbol">♜</span> Reign
        </Link>
        <nav className="site-nav" aria-label="主导航">
          {links.map(([href, title]) => (
            <Link
              key={href}
              href={href}
              aria-current={pathname === href ? "page" : undefined}
            >
              {title}
            </Link>
          ))}
        </nav>
        <div className="site-header-actions">
          <Link className="site-console-link" href="/chat">
            控制台 <ArrowUpRight size={14} />
          </Link>
          <Link className="button primary site-download-link" href="/download">
            下载客户端
          </Link>
        </div>
        <details className="site-mobile-menu" key={pathname}>
          <summary aria-label="展开导航">
            <Menu size={22} />
          </summary>
          <nav aria-label="移动导航">
            {links.map(([href, title]) => (
              <Link
                key={href}
                href={href}
                aria-current={pathname === href ? "page" : undefined}
              >
                {title}
              </Link>
            ))}
            <Link href="/download">下载客户端</Link>
            <Link href="/chat">打开控制台 ↗</Link>
          </nav>
        </details>
      </div>
    </header>
  );
}
