import type { NextConfig } from "next";
import { initOpenNextCloudflareForDev } from "@opennextjs/cloudflare";
if (process.env.NODE_ENV === "development") void initOpenNextCloudflareForDev();
const config: NextConfig = {
  transpilePackages: [
    "@reign/web-client",
    "@reign/client-sdk",
    "@reign/protocol",
  ],
  images: { unoptimized: true },
  poweredByHeader: false,
};
export default config;
