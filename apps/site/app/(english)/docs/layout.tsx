import Layout from "../../[lang]/docs/layout";
export default function EnglishDocsLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <Layout params={Promise.resolve({ lang: "en" })}>{children}</Layout>;
}
