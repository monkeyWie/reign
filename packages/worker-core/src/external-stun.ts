import { APIError } from "@reignhub/protocol";
import { configArray, transportEndpoints } from "./endpoint";
import type { CoreEnv } from "./types";

function invalidConfig() {
  return new APIError(
    "INVALID_STUN_CONFIG",
    "EXTERNAL_STUN 探测节点配置无效",
    503,
  );
}

export function externalStun(env: CoreEnv) {
  const config: unknown = env.EXTERNAL_STUN;
  if (config === undefined) {
    const split = (s = "") =>
      s
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean);
    return { selfStun: split(env.SELF_STUN), tcpStun: split(env.TCP_STUN) };
  }
  const { udp, tcp } = transportEndpoints(
    configArray(config, invalidConfig),
    invalidConfig,
  );
  return { selfStun: udp.map((address) => "stun:" + address), tcpStun: tcp };
}
