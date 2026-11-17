import {
  handleConsole,
  handleCore,
  RelayRoom,
  type CoreEnv,
} from "@reignhub/worker-core";
export { RelayRoom };
export default {
  async fetch(request: Request, env: CoreEnv & { ASSETS: Fetcher }) {
    const url = new URL(request.url);
    if (url.pathname === "/") {
      url.pathname = "/chat/";
      return new Response(null, {
        status: 302,
        headers: { Location: url.href, "Cache-Control": "no-store" },
      });
    }
    if (url.pathname === "/healthz")
      return new Response("ok", { headers: { "Cache-Control": "no-store" } });
    return (
      (await handleConsole(request, env)) ??
      (await handleCore(request, env)) ??
      new Response("Not found", { status: 404 })
    );
  },
  async scheduled(_event: ScheduledController, env: CoreEnv) {
    await env.DB.prepare("DELETE FROM pairings WHERE expires < ?")
      .bind(Math.floor(Date.now() / 1000) - 86400)
      .run();
  },
} satisfies ExportedHandler<CoreEnv & { ASSETS: Fetcher }>;
