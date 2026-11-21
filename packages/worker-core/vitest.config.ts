import { generateKeyPairSync } from "node:crypto";
import { defineConfig } from "vitest/config";
import {
  cloudflareTest,
  readD1Migrations,
} from "@cloudflare/vitest-pool-workers";
const signer = generateKeyPairSync("ed25519").privateKey.export({
  format: "jwk",
});
export default defineConfig({
  plugins: [
    cloudflareTest({
      main: "./test/worker.ts",
      miniflare: {
        compatibilityDate: "2026-03-17",
        compatibilityFlags: ["nodejs_compat"],
        d1Databases: ["DB"],
        durableObjects: { ROOMS: { className: "RelayRoom", useSQLite: true } },
        bindings: {
          TEST_MIGRATIONS: await readD1Migrations("./migrations"),
          TEST_RELAY_SIGNER: signer,
          EXTERNAL_STUN: ["probe.example:26331"],
          EXTERNAL_RELAYS: [
            {
              addresses: ["udp:relay.example:26332", "tcp:relay.example:26333"],
              credential: `rh1.${signer.x}.${signer.d}`,
            },
          ],
        },
      },
    }),
  ],
  test: { include: ["test/**/*.test.ts"] },
});
