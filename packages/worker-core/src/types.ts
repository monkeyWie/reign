import type { Decision, Role } from "@reign/protocol";
import type { DeviceRow } from "./schema";
export interface CoreEnv {
  DB: D1Database;
  ROOMS: DurableObjectNamespace;
  ALLOWED_ORIGINS?: string;
  PUBLIC_STUN?: string;
  SELF_STUN?: string;
  TCP_STUN?: string;
  PUBLIC_URL?: string;
}
export interface Policy {
  deployment: "self-hosted" | "saas";
  loginProviders(env: CoreEnv): string[];
  admit(
    env: CoreEnv,
    device: DeviceRow,
    host: DeviceRow,
    operation: "connect" | "renew" | "relay",
    role: Role,
  ): Promise<Decision>;
  release?(env: CoreEnv, device: DeviceRow, host: DeviceRow): Promise<void>;
}
export const unrestricted: Policy = {
  deployment: "self-hosted",
  loginProviders: () => [],
  async admit(_env, _device, host) {
    return {
      limits: {
        maxHostDevices: null,
        maxAgentsPerDevice: null,
        maxConcurrentConnections: null,
        allowRelay: true,
      },
      products: null,
      quotaKey: host.id,
      revision: "self-hosted",
    };
  },
};
