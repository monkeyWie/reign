import { ArrowUpRight, Globe, Monitor, Smartphone } from "lucide-react";
import { translate, type Locale } from "../lib/i18n";
import { LocalizedLink as Link } from "./locale";

export function PlatformShowcase({ lang }: { lang: Locale }) {
  const t = translate(lang);
  return (
    <section className="platform-section site-width">
      <div className="platform-heading">
        <div>
          <span className="site-eyebrow">SIX PLATFORMS. ONE REIGN.</span>
          <h2>{t("每一块屏幕，都是你的主场。")}</h2>
        </div>
        <p>{t("桌面原生，移动随行，浏览器即开即用。")}</p>
      </div>
      <div className="platform-grid">
        {[
          [Monitor, "macOS", t("原生桌面")],
          [Monitor, "Windows", t("原生桌面")],
          [Monitor, "Linux", t("原生桌面")],
          [Smartphone, "iOS", t("移动掌控")],
          [Smartphone, "Android", t("移动掌控")],
          [Globe, "Web", t("打开即连接")],
        ].map(([Icon, name, caption]) => {
          const I = Icon as typeof Monitor;
          return (
            <Link
              href={name === "Web" ? "/chat" : "/download"}
              className="platform-card"
              key={String(name)}
            >
              <I size={25} strokeWidth={1.4} />
              <strong>{String(name)}</strong>
              <span>{String(caption)}</span>
              <ArrowUpRight className="platform-arrow" size={14} />
            </Link>
          );
        })}
      </div>
    </section>
  );
}
