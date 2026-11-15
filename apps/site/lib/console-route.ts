// The console is a standalone NSGo app. It has no Next/React hydration bundle.
export async function handleConsole(
  request: Request,
  env: { ASSETS?: { fetch(request: Request): Promise<Response> } },
): Promise<Response | undefined> {
  const url = new URL(request.url);
  if (!["/chat", "/chat/"].includes(url.pathname)) return;
  if (!["GET", "HEAD"].includes(request.method))
    return new Response("Method not allowed", {
      status: 405,
      headers: { Allow: "GET, HEAD" },
    });
  if (!env.ASSETS)
    return new Response("Workspace assets unavailable", { status: 503 });
  url.pathname = "/assets/chat/index.html";
  url.search = "";
  const asset = await env.ASSETS.fetch(
    new Request(url, { method: request.method }),
  );
  const headers = new Headers(asset.headers);
  headers.set("Cache-Control", "no-cache");
  headers.set("X-Robots-Tag", "noindex, nofollow");
  return new Response(asset.body, { status: asset.status, headers });
}
