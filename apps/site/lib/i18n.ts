import { publicLocalePath } from "./language-routing";
import { defineI18n } from "fumadocs-core/i18n";
import en from "./locales/en.json";

export const locales = ["en", "zh-CN"] as const;
export type Locale = (typeof locales)[number];
export const i18n = defineI18n({
  defaultLanguage: "en",
  languages: [...locales],
  fallbackLanguage: null,
  hideLocale: "default-locale",
});
export function isLocale(value: string): value is Locale {
  return locales.includes(value as Locale);
}
export function translate(locale: Locale) {
  return (key: string) =>
    locale === "en" ? ((en as Record<string, string>)[key] ?? key) : key;
}
export function localizeHref(href: string, locale: Locale) {
  if (
    /^\/(?:$|features(?:[/?#]|$)|technology(?:[/?#]|$)|pricing(?:[/?#]|$)|download(?:[/?#]|$)|terms(?:[/?#]|$)|privacy(?:[/?#]|$)|docs(?:[/?#]|$))/.test(
      href,
    )
  ) {
    return publicLocalePath(href, locale);
  }
  return href;
}
export function switchLocale(pathname: string, locale: Locale) {
  const path = pathname.replace(/^\/zh-CN(?=\/|$)/, "");
  return localizeHref(path || "/", locale);
}
