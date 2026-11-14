"use client";
import { legalLinks } from "../../../lib/legal";
import { AccountPage } from "@reign/web-client";
export default function Account() {
  return <AccountPage legal={legalLinks("zh-CN")} />;
}
