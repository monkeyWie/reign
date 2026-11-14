import Layout from "../[lang]/layout";
export default function EnglishLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <Layout params={Promise.resolve({ lang: "en" })}>{children}</Layout>;
}
