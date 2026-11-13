import { x25519 } from "@noble/curves/ed25519.js";
const e = new TextEncoder();
export const utf8 = (s: string) => e.encode(s);
export const decode = (b: Uint8Array) => new TextDecoder().decode(b);
export const hex = (b: Uint8Array) =>
  [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
export function unhex(s: string) {
  if (!/^(?:[a-f0-9]{2})+$/.test(s)) throw Error("无效密钥");
  return Uint8Array.from(s.match(/../g)!, (x) => parseInt(x, 16));
}
export function b64(b: Uint8Array) {
  let s = "";
  for (const x of b) s += String.fromCharCode(x);
  return btoa(s);
}
export const un64 = (s: string) =>
  Uint8Array.from(atob(s.replaceAll("-", "+").replaceAll("_", "/")), (x) =>
    x.charCodeAt(0),
  );
export const url64 = (b: Uint8Array) =>
  b64(b).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
export const random = (n = 32) => crypto.getRandomValues(new Uint8Array(n));
export async function sha(b: Uint8Array) {
  return new Uint8Array(
    await crypto.subtle.digest("SHA-256", b as BufferSource),
  );
}
export async function sum(s: string) {
  return hex(await sha(utf8(s)));
}
export function keypair() {
  const privateKey = random();
  return {
    private: hex(privateKey),
    public: hex(x25519.getPublicKey(privateKey)),
  };
}
async function aes(raw: Uint8Array) {
  return crypto.subtle.importKey("raw", raw as BufferSource, "AES-GCM", false, [
    "encrypt",
    "decrypt",
  ]);
}
export async function envelope(
  privateKey: string,
  publicKey: string,
  context: string,
  value?: unknown,
  packet?: string,
): Promise<unknown> {
  const shared = x25519.getSharedSecret(unhex(privateKey), unhex(publicKey));
  const hk = await crypto.subtle.importKey(
    "raw",
    shared as BufferSource,
    "HKDF",
    false,
    ["deriveBits"],
  );
  const raw = await crypto.subtle.deriveBits(
    {
      name: "HKDF",
      hash: "SHA-256",
      salt: utf8("reign/pairing"),
      info: utf8(context),
    },
    hk,
    256,
  );
  const key = await aes(new Uint8Array(raw));
  if (packet !== undefined) {
    const b = un64(packet);
    const plain = await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: b.slice(0, 12), additionalData: utf8(context) },
      key,
      b.slice(12),
    );
    return JSON.parse(decode(new Uint8Array(plain)));
  }
  const nonce = random(12);
  const sealed = new Uint8Array(
    await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: nonce, additionalData: utf8(context) },
      key,
      utf8(JSON.stringify(value)),
    ),
  );
  return b64(new Uint8Array([...nonce, ...sealed]));
}
export class Cipher {
  private counter = 0n;
  private highest = 0n;
  private seen = new Set<bigint>();
  private constructor(
    private key: CryptoKey,
    private prefix: number,
    private randomNonce: boolean,
  ) {}
  static async create(secret: string, prefix = 1, randomNonce = false) {
    const raw = await sha(
      new Uint8Array([...utf8("reign/p2p/aead/"), ...unhex(secret)]),
    );
    return new Cipher(await aes(raw), prefix, randomNonce);
  }
  async seal(aad: string, plain: Uint8Array) {
    const nonce = this.randomNonce ? random(12) : new Uint8Array(12);
    if (!this.randomNonce) {
      nonce[0] = this.prefix;
      new DataView(nonce.buffer).setBigUint64(4, ++this.counter);
    }
    const body = new Uint8Array(
      await crypto.subtle.encrypt(
        { name: "AES-GCM", iv: nonce, additionalData: utf8(aad) },
        this.key,
        plain as BufferSource,
      ),
    );
    return new Uint8Array([...nonce, ...body]);
  }
  async open(aad: string, packet: Uint8Array) {
    if (packet.length < 28) throw Error("无效加密帧");
    const nonce = packet.slice(0, 12);
    const count = new DataView(nonce.buffer).getBigUint64(4);
    if (
      !this.randomNonce &&
      (nonce[0] === this.prefix ||
        nonce[1] ||
        nonce[2] ||
        nonce[3] ||
        !count ||
        this.seen.has(count) ||
        count + 4096n <= this.highest)
    )
      throw Error("重复加密帧");
    const plain = new Uint8Array(
      await crypto.subtle.decrypt(
        { name: "AES-GCM", iv: nonce, additionalData: utf8(aad) },
        this.key,
        packet.slice(12),
      ),
    );
    if (!this.randomNonce) {
      if (this.seen.has(count)) throw Error("重复加密帧");
      this.seen.add(count);
      if (count > this.highest) this.highest = count;
      for (const n of this.seen)
        if (n + 4096n <= this.highest) this.seen.delete(n);
    }
    return plain;
  }
}
