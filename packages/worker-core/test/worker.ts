import { handleCore, RelayRoom, type CoreEnv } from "../src";
export { RelayRoom };
export default {
  fetch: async (r: Request, e: CoreEnv) =>
    (await handleCore(r, e)) ?? new Response("not found", { status: 404 }),
};
