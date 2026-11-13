import { createFromSource } from "fumadocs-core/search/server";
import { source } from "../../../lib/source";

// Build once; search runs in the browser, with multilingual segmentation.
export const dynamic = "force-static";
export const { staticGET: GET } = createFromSource(source);
