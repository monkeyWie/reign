import type { Locale } from "./i18n";

// Optional links owned by the deployment operator. No hosted-service policy or
// legal identity is shipped in the open-source client.
export function legalLinks(locale: Locale) {
  const resolve = (value: string | undefined) => {
    if (!value) return undefined;
    const href = value
      .replaceAll("{localePrefix}", locale === "en" ? "" : "/zh-CN")
      .replaceAll("{lang}", locale);
    if (href.startsWith("/") && !href.startsWith("//")) return href;
    try {
      return ["https:", "http:"].includes(new URL(href).protocol)
        ? href
        : undefined;
    } catch {
      return undefined;
    }
  };
  const terms = resolve(process.env.NEXT_PUBLIC_TERMS_URL);
  const privacy = resolve(process.env.NEXT_PUBLIC_PRIVACY_URL);
  return terms && privacy ? { terms, privacy } : undefined;
}
