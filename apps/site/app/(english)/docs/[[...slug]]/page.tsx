import Page, {
  generateMetadata as metadata,
} from "../../../[lang]/docs/[[...slug]]/page";
import { source } from "../../../../lib/source";
type Props = { params: Promise<{ slug?: string[] }> };
export default async function EnglishDocsPage({ params }: Props) {
  return <Page params={Promise.resolve({ ...(await params), lang: "en" })} />;
}
export function generateStaticParams() {
  return source.getPages("en").map((page) => ({ slug: page.slugs }));
}
export async function generateMetadata({ params }: Props) {
  return metadata({
    params: Promise.resolve({ ...(await params), lang: "en" }),
  });
}
