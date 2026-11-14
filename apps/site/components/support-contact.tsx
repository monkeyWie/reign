"use client";
import { supportEmail } from "../lib/contact";
import { translate, type Locale } from "../lib/i18n";
import { useLocale } from "./locale";

export function SupportContact({
  className,
  locale,
}: {
  className?: string;
  locale?: Locale;
}) {
  const currentLocale = useLocale();
  const t = translate(locale ?? currentLocale);
  const email = supportEmail();
  if (!email) return null;
  return (
    <a className={className} href={`mailto:${email}`}>
      {t("客服邮箱")}: {email}
    </a>
  );
}
