import { pageMetadata } from "../../../../lib/seo";
import { isLocale } from "../../../../lib/i18n";
import {
  DocsBody,
  DocsDescription,
  DocsPage,
  DocsTitle,
  EditOnGitHub,
} from "fumadocs-ui/layouts/docs/page";
import { createRelativeLink } from "fumadocs-ui/mdx";
import { notFound } from "next/navigation";
import type { Metadata } from "next";
import { source } from "../../../../lib/source";
import { getMDXComponents } from "../../../../components/mdx";
import { DocsFooter } from "../../../../components/docs-footer";

type Props = { params: Promise<{ slug?: string[]; lang: string }> };
export default async function Page({ params }: Props) {
  const { slug, lang } = await params;
  if (!isLocale(lang)) notFound();
  const page = source.getPage(slug, lang);
  if (!page) notFound();
  const MDX = page.data.body;
  return (
    <DocsPage
      toc={page.data.toc}
      full={page.data.full}
      slots={{ footer: DocsFooter }}
    >
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription>{page.data.description}</DocsDescription>
      <DocsBody>
        <MDX
          components={getMDXComponents(
            { a: createRelativeLink(source, page) },
            lang,
          )}
        />
      </DocsBody>
      <EditOnGitHub
        href={`https://github.com/monkeyWie/reign/edit/main/apps/site/content/docs/${page.path}`}
      >
        {lang === "en" ? "Edit this page on GitHub" : "在 GitHub 上编辑此页"}
      </EditOnGitHub>
    </DocsPage>
  );
}
export function generateStaticParams() {
  return source.generateParams().filter((params) => params.lang === "zh-CN");
}
export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { slug, lang } = await params;
  if (!isLocale(lang)) notFound();
  const page = source.getPage(slug, lang);
  if (!page) notFound();
  return pageMetadata(
    lang,
    `/docs${slug?.length ? "/" + slug.join("/") : ""}`,
    `${page.data.title} · Reign ${lang === "en" ? "Docs" : "文档"}`,
    page.data.description ?? "",
  );
}
