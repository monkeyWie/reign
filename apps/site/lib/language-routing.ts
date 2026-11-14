// Shared by the self-hosted and official Workers. Routing never needs account
// data and does not change API, P2P, console or static-asset requests.
export const localeCookie = "reignhub_locale";
export type SiteLocale = "en" | "zh-CN";
export function isSiteLocale(value: unknown): value is SiteLocale {
  return value === "en" || value === "zh-CN";
}
export function publicLocalePath(path: string, locale: SiteLocale): string {
  const bare = path.replace(/^\/zh-CN(?=\/|$)/, "") || "/";
  return locale === "en" ? bare : "/zh-CN" + (bare === "/" ? "" : bare);
}
export function sitePage(path: string): boolean {
  const bare = path.replace(/^\/zh-CN(?=\/|$)/, "") || "/";
  return /^\/(?:$|features\/?$|technology\/?$|pricing\/?$|download\/?$|terms\/?$|privacy\/?$|docs(?:\/|$))/.test(
    bare,
  );
}
export function preferredLocale(header: string | null): SiteLocale {
  const choices = (header || "")
    .split(",")
    .map((part, index) => {
      const [tag, ...params] = part.trim().split(";");
      const q = params.find((value) => value.trim().startsWith("q="));
      return {
        tag: tag.toLowerCase(),
        quality: q ? Number(q.trim().slice(2)) : 1,
        index,
      };
    })
    .filter(
      ({ quality }) => Number.isFinite(quality) && quality > 0 && quality <= 1,
    )
    .sort((a, b) => b.quality - a.quality || a.index - b.index);
  for (const { tag } of choices) {
    if (/^zh(?:-|$)/.test(tag)) return "zh-CN";
    if (/^en(?:-|$)/.test(tag) || tag === "*") return "en";
  }
  return "en";
}
export function rememberedLocale(request: Request): SiteLocale | undefined {
  const value = request.headers
    .get("Cookie")
    ?.split(";")
    .map((item) => item.trim())
    .find((item) => item.startsWith(localeCookie + "="))
    ?.slice(localeCookie.length + 1);
  return isSiteLocale(value) ? value : undefined;
}
function cookie(request: Request, locale: SiteLocale): string {
  return `${localeCookie}=${locale}; Path=/; Max-Age=31536000; SameSite=Lax${new URL(request.url).protocol === "https:" ? "; Secure" : ""}`;
}
function redirect(url: URL, status: number, preference?: string) {
  const headers = new Headers({
    Location: url.href,
    "Cache-Control": "private, no-store",
    Vary: "Accept-Language, Cookie",
  });
  if (preference) headers.append("Set-Cookie", preference);
  return new Response(null, { status, headers });
}
export async function withSiteLanguage(
  request: Request,
  serve: () => Response | Promise<Response>,
): Promise<Response> {
  const url = new URL(request.url);
  if (!["GET", "HEAD"].includes(request.method) || !sitePage(url.pathname))
    return serve();
  // RSC and prefetch fetches must never choose a language or write preferences.
  const navigation =
    request.headers.get("RSC") !== "1" &&
    request.headers.get("Next-Router-Prefetch") !== "1" &&
    !/prefetch/i.test(request.headers.get("Purpose") || "");
  const explicit = url.searchParams.get("lang");
  if (navigation && isSiteLocale(explicit)) {
    url.searchParams.delete("lang");
    url.pathname = publicLocalePath(url.pathname, explicit);
    return redirect(url, 303, cookie(request, explicit));
  }
  const chinese = /^\/zh-CN(?:\/|$)/.test(url.pathname);
  const remembered = rememberedLocale(request);
  const locale = chinese
    ? "zh-CN"
    : (remembered ?? preferredLocale(request.headers.get("Accept-Language")));
  if (navigation && !chinese && locale === "zh-CN") {
    url.pathname = publicLocalePath(url.pathname, "zh-CN");
    return redirect(
      url,
      307,
      !remembered ? cookie(request, locale) : undefined,
    );
  }
  const response = await serve();
  if (
    !navigation ||
    response.status !== 200 ||
    !response.headers.get("Content-Type")?.includes("text/html")
  )
    return response;
  const headers = new Headers(response.headers);
  if (!remembered)
    headers.append("Set-Cookie", cookie(request, chinese ? "zh-CN" : "en"));
  if (!chinese || !remembered) {
    headers.set("Cache-Control", "private, no-store");
    const vary = new Set(
      (headers.get("Vary") || "")
        .split(",")
        .map((v) => v.trim())
        .filter(Boolean),
    );
    vary.add("Accept-Language");
    vary.add("Cookie");
    headers.set("Vary", [...vary].join(", "));
  }
  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}
