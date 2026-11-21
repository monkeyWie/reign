import { APIError } from "@reignhub/protocol";
import { configArray, transportEndpoints } from "./endpoint";
import { bytes64, un64 } from "./crypto";
import type { CoreEnv } from "./types";

export interface RelayNode {
  address?: string;
  udpAddress?: string;
  tcpAddress?: string;
  pin: string;
}

export function externalRelays(env: CoreEnv) {
  const error = () =>
    new APIError("INVALID_RELAY_CONFIG", "外部中继配置无效", 503);
  const entries = configArray(
    env.EXTERNAL_RELAYS === undefined ? [] : env.EXTERNAL_RELAYS,
    error,
  );
  const seen = new Set<string>();
  return entries.map((entry: unknown) => {
    if (
      !entry ||
      typeof entry !== "object" ||
      !("addresses" in entry) ||
      Object.keys(entry).some((k) => k !== "addresses" && k !== "credential") ||
      !("credential" in entry) ||
      typeof entry.credential !== "string"
    )
      throw error();
    const { addresses, credential } = entry;
    const match = /^rh1\.([A-Za-z0-9_-]{43})\.([A-Za-z0-9_-]{43})$/.exec(
      credential,
    );
    if (!match || bytes64(un64(match[2])) !== match[2]) throw error();
    const [, pin, secret] = match;
    const { udp, tcp } = transportEndpoints(addresses, error);
    if (
      udp.length + tcp.length === 0 ||
      udp.length > 1 ||
      tcp.length > 1 ||
      !/^[a-zA-Z0-9_-]{43}$/.test(pin) ||
      bytes64(un64(pin)) !== pin ||
      seen.has(pin)
    )
      throw error();
    seen.add(pin);
    const endpoint: RelayNode =
      udp[0] === tcp[0]
        ? { address: udp[0], pin }
        : { udpAddress: udp[0], tcpAddress: tcp[0], pin };
    return { endpoint, secret };
  });
}

export async function relayWelcome(
  node: ReturnType<typeof externalRelays>[number],
  host: string,
  generation: string,
  peer: string,
  role: string,
  expires: number,
) {
  const until = Math.min(expires, Math.floor(Date.now() / 1000) + 60);
  const payload = ["rh2", host, generation, peer, role, until].join(".");
  const key = await crypto.subtle.importKey(
    "raw",
    un64(node.secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const mac = await crypto.subtle.sign(
    "HMAC",
    key,
    new TextEncoder().encode(payload),
  );
  return {
    ...node.endpoint,
    ticket: payload + "." + bytes64(new Uint8Array(mac)),
  };
}
