import { handleCore, RelayRoom, type CoreEnv } from "@reign/worker-core";
export { RelayRoom };
export default {
  async fetch(request: Request, env: CoreEnv) {
    return (
      (await handleCore(request, env)) ??
      new Response(
        new URL(request.url).pathname === "/healthz"
          ? "ok"
          : "Reign self-hosted service",
        { headers: { "Cache-Control": "no-store" } },
      )
    );
  },
} satisfies ExportedHandler<CoreEnv>;
