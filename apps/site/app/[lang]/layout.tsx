import { notFound } from "next/navigation";
import { LocaleProvider } from "../../components/locale";
import { isLocale } from "../../lib/i18n";
import "../globals.css";

export const dynamicParams = false;

export function generateStaticParams() {
  return [{ lang: "zh-CN" }];
}
export default async function RootLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ lang: string }>;
}) {
  const { lang } = await params;
  if (!isLocale(lang)) notFound();
  return (
    <html lang={lang} suppressHydrationWarning>
      <body>
        <LocaleProvider locale={lang}>{children}</LocaleProvider>
      </body>
    </html>
  );
}
