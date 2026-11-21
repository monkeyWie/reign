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

export function externalRelays(env: CoreEnv): RelayNode[] {
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
      Object.keys(entry).some((k) => k !== "addresses" && k !== "pin") ||
      !("pin" in entry) ||
      typeof entry.pin !== "string"
    )
      throw error();
    const { addresses, pin } = entry;
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
    return udp[0] === tcp[0]
      ? { address: udp[0], pin }
      : { udpAddress: udp[0], tcpAddress: tcp[0], pin };
  });
}

export async function verifyRelayCheck(
  env: CoreEnv,
  headers: Headers,
  raw: string,
): Promise<string> {
  const pin = headers.get("X-Reign-Relay-Pin");
  const signature = headers.get("X-Reign-Relay-Signature") || "";
  if (
    pin &&
    externalRelays(env).some((node) => node.pin === pin) &&
    /^[A-Za-z0-9_-]{86}$/.test(signature)
  ) {
    const key = await crypto.subtle.importKey(
      "raw",
      un64(pin),
      "Ed25519",
      false,
      ["verify"],
    );
    if (
      await crypto.subtle.verify(
        "Ed25519",
        key,
        un64(signature),
        new TextEncoder().encode("reign/native-relay/lease/v1\n" + raw),
      )
    )
      return pin;
  }
  throw new APIError("RELAY_DENIED", "中继验证失败", 403);
}
