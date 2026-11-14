import Page, { generateMetadata as metadata } from "../../[lang]/pricing/page";
export default function EnglishPage() {
  return <Page params={Promise.resolve({ lang: "en" })} />;
}
export function generateMetadata() {
  return metadata({ params: Promise.resolve({ lang: "en" }) });
}
