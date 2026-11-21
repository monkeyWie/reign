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

function discoveryList(value: unknown) {
  const { udp, tcp } = transportEndpoints(
    configArray(value, invalidConfig),
    invalidConfig,
  );
  return { selfStun: udp.map((address) => "stun:" + address), tcpStun: tcp };
}

const builtInPublicStun = [
  "stun:stun.cloudflare.com:3478",
  "stun:stun.l.google.com:19302",
  "stun:stun.chat.bilibili.com:3478",
];

function split(value = "") {
  return value
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);
}

export function stunServers(env: CoreEnv) {
  const publicStun = new Set([...builtInPublicStun, ...split(env.PUBLIC_STUN)]);
  const entries =
    env.EXTERNAL_STUN !== undefined ? env.EXTERNAL_STUN : env.STUN_SERVERS;
  const { selfStun, tcpStun } =
    entries === undefined
      ? { selfStun: split(env.SELF_STUN), tcpStun: split(env.TCP_STUN) }
      : discoveryList(entries);
  return {
    publicStun: [...publicStun],
    selfStun: selfStun.filter((url) => !publicStun.has(url)),
    tcpStun,
  };
}
