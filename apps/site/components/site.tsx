import Link from "next/link";
import "./site.css";
import { ArrowUpRight } from "lucide-react";
import type { ReactNode } from "react";
import { SiteHeader } from "./site-header";

export function SiteShell({ children }: { children: ReactNode }) {
  return (
    <div className="site-shell">
      <a className="site-skip" href="#main">
        跳到主要内容
      </a>
      <SiteHeader />
      {children}
      <SiteFooter />
    </div>
  );
}
export function SiteFooter() {
  return (
    <footer className="site-footer site-width">
      <div className="footer-top">
        <div>
          <Link className="brand" href="/">
            ♜ Reign
          </Link>
          <p>
            你的 Agent，你的电脑。
            <br />
            从任何屏幕，继续你的工作。
          </p>
          <span className="footer-caption">Your agents. Anywhere.</span>
        </div>
        {[
          [
            "产品",
            [
              ["特性", "/features"],
              ["技术栈", "/technology"],
              ["价格", "/pricing"],
              ["下载", "/download"],
            ],
          ],
          [
            "文档",
            [
              ["快速开始", "/docs/quick-start"],
              ["Agent 接入", "/docs/agents"],
              ["自建服务", "/docs/self-hosting"],
              ["常见问题", "/docs/guides/troubleshooting"],
            ],
          ],
          [
            "开发者",
            [
              ["GitHub", "https://github.com/monkeyWie/reign"],
              ["贡献指南", "/docs/contributing"],
              ["连接协议", "/docs/architecture/protocol"],
              ["安全与隐私", "/docs/privacy"],
            ],
          ],
        ].map(([title, items]) => (
          <div className="footer-links" key={String(title)}>
            <strong>{String(title)}</strong>
            {(items as string[][]).map(([label, href]) => (
              <Link key={href} href={href}>
                {label}
              </Link>
            ))}
          </div>
        ))}
      </div>
      <div className="footer-bottom">
        <span>© {new Date().getFullYear()} Reign · 开源核心采用 MIT 许可</span>
        <span>原生客户端 · Web 控制台 · 可自行部署</span>
      </div>
    </footer>
  );
}
export function PageIntro({
  eyebrow,
  title,
  children,
}: {
  eyebrow: string;
  title: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="page-intro site-width">
      <span className="site-eyebrow">{eyebrow}</span>
      <h1>{title}</h1>
      <p>{children}</p>
    </section>
  );
}
export function SectionHeading({
  eyebrow,
  title,
  children,
}: {
  eyebrow: string;
  title: string;
  children?: ReactNode;
}) {
  return (
    <div className="section-heading">
      <span className="site-eyebrow">{eyebrow}</span>
      <h2>{title}</h2>
      {children && <p>{children}</p>}
    </div>
  );
}
export function BottomCTA() {
  return (
    <section className="site-cta site-width">
      <div>
        <span className="site-eyebrow">TAKE YOUR WORK WITH YOU</span>
        <h2>下一次灵感，不必等到回到电脑前。</h2>
        <p>保留熟悉的 Agent，让你的设备连在一起。</p>
      </div>
      <Link className="button primary" href="/docs/quick-start">
        开始使用 <ArrowUpRight size={16} />
      </Link>
    </section>
  );
}
