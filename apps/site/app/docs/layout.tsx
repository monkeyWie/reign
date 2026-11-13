import { DocsProvider } from "../../components/docs-provider";
import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { source } from "../../lib/source";
import type { ReactNode } from "react";
import "./docs.css";

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <DocsProvider>
      <DocsLayout
        tree={source.getPageTree()}
        nav={{
          title: (
            <span className="brand">
              ♜ Reign <span className="docs-wordmark">/ 文档</span>
            </span>
          ),
          url: "/",
        }}
        githubUrl="https://github.com/monkeyWie/reign"
        links={[
          { text: "特性", url: "/features" },
          { text: "技术栈", url: "/technology" },
          { text: "价格", url: "/pricing" },
          { text: "打开控制台 ↗", url: "/chat" },
        ]}
      >
        {children}
      </DocsLayout>
    </DocsProvider>
  );
}
