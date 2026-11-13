import type { Metadata } from "next";
import "./globals.css";
export const metadata: Metadata = {
  title: "Reign — 你的 Agent，随处掌控",
  description:
    "原生桌面与手机客户端，加上 Web 控制台，远控自己电脑上的 Codex App 与 CLI。直连优先、端到端加密，支持自行部署。",
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
