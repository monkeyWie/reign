"use client";
import { legalLinks } from "../../../lib/legal";
import { AccountPage } from "@reignhub/account-ui";
import { supportEmail } from "../../../lib/contact";
import { SupportContact } from "../../../components/support-contact";
export default function Account() {
  return (
    <>
      <AccountPage legal={legalLinks("zh-CN")} />
      {supportEmail() && (
        <footer className="account-support-footer">
          <SupportContact locale="zh-CN" />
        </footer>
      )}
    </>
  );
}
