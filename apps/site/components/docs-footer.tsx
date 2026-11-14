"use client";
import { PageFooter, type FooterProps } from "fumadocs-ui/layouts/docs/page";
import { supportEmail } from "../lib/contact";
import { SupportContact } from "./support-contact";

export function DocsFooter(props: FooterProps) {
  return (
    <div>
      <PageFooter {...props} />
      {supportEmail() && (
        <footer className="docs-support-footer">
          <SupportContact />
        </footer>
      )}
    </div>
  );
}
