// Native discovery and relay listeners are advertised as host:port pairs.
export function validEndpoint(address: string): boolean {
  const port = /^(?:[a-zA-Z0-9.-]+|\[[a-fA-F0-9:.]+\]):([0-9]+)$/.exec(
    address,
  )?.[1];
  if (!port || +port === 0 || +port > 65535) return false;
  if (address.startsWith("[")) {
    try {
      new URL("https://" + address);
    } catch {
      return false;
    }
  }
  return true;
}

export function configArray(input: unknown, invalid: () => Error): unknown[] {
  let entries = input;
  if (typeof entries === "string") {
    try {
      entries = JSON.parse(entries);
    } catch {
      throw invalid();
    }
  }
  if (!Array.isArray(entries) || entries.length > 32) throw invalid();
  return entries;
}

// Only the Worker binding itself can be JSON text. Nested values are native arrays.
export function transportEndpoints(entries: unknown, invalid: () => Error) {
  if (!Array.isArray(entries) || entries.length > 32) throw invalid();
  const udp = new Set<string>(),
    tcp = new Set<string>();
  for (const entry of entries) {
    if (typeof entry !== "string") throw invalid();
    const udpOnly = entry.startsWith("udp:"),
      tcpOnly = entry.startsWith("tcp:");
    const address = udpOnly || tcpOnly ? entry.slice(4) : entry;
    if (!validEndpoint(address)) throw invalid();
    if (!tcpOnly) udp.add(address);
    if (!udpOnly) tcp.add(address);
  }
  return { udp: [...udp], tcp: [...tcp] };
}
