interface BrowserInfo {
  userAgent: string;
  userAgentData?: {
    getHighEntropyValues?: (hints: string[]) => Promise<{
      fullVersionList?: { brand: string; version: string }[];
      platform?: string;
      platformVersion?: string;
    }>;
  };
}
// Use the version the browser actually exposes (UA reduction may hide patch versions).
export function browserName(
  ua: string,
  versions?: { brand: string; version: string }[],
  platform?: string,
) {
  const browsers = [
    ["Edge", /(?:Edg|EdgA|EdgiOS)\/([\d.]+)/, "Microsoft Edge"],
    ["Opera", /(?:OPR|OPiOS)\/([\d.]+)/, "Opera"],
    ["Firefox", /(?:Firefox|FxiOS)\/([\d.]+)/, "Firefox"],
    ["Chrome", /(?:Chrome|CriOS)\/([\d.]+)/, "Google Chrome"],
    ["Safari", /Version\/([\d.]+).*Safari\//, "Safari"],
  ] as const;
  let browser = "Browser";
  for (const [name, pattern, brand] of browsers) {
    const match = ua.match(pattern);
    if (match) {
      browser = `${name} ${versions?.find((v) => v.brand === brand)?.version || match[1]}`;
      break;
    }
  }
  let os = platform || "";
  if (/Android/.test(ua))
    os = `Android ${ua.match(/Android ([\d.]+)/)?.[1] || ""}`.trim();
  else if (/(iPhone|iPad|iPod)/.test(ua))
    os =
      `iOS ${ua.match(/OS ([\d_]+)/)?.[1]?.replaceAll("_", ".") || ""}`.trim();
  else if (/Windows/.test(ua)) os = platform || "Windows";
  else if (/Macintosh|Mac OS X/.test(ua)) os = "macOS";
  else if (/Linux/.test(ua)) os = "Linux";
  return (browser + (os ? ` · ${os}` : "")).slice(0, 100);
}
export async function browserDeviceName(
  info: BrowserInfo | undefined = typeof navigator === "undefined"
    ? undefined
    : (navigator as BrowserInfo),
) {
  if (!info) return "Browser";
  let hints;
  try {
    hints = await info.userAgentData?.getHighEntropyValues?.([
      "fullVersionList",
      "platform",
      "platformVersion",
    ]);
  } catch {
    /* UA fallback also works on HTTP and browsers without client hints. */
  }
  return browserName(info.userAgent, hints?.fullVersionList, hints?.platform);
}
