import type { Metadata } from "next";
import "./globals.css";
export const metadata: Metadata = {
  title: "Reign — 让 Agent 随你而行",
  description:
    "一个客户端，远控你所有设备上的 AI Agent。优先直连、端到端加密、支持自行部署。",
};
export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <body>{children}</body>
    </html>
  );
}
