import type { Metadata } from "next";
import { NavigationProgress } from "../../components/navigation-progress";
import "../globals.css";
export const metadata: Metadata = {
  title: "ReignHub",
  robots: { index: false, follow: false },
};
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>
        <NavigationProgress />
        {children}
      </body>
    </html>
  );
}
