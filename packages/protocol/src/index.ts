export const PROTOCOL = 1;
export const SOCKET_PROTOCOL = "reign.p2p";
export const routes = {
  config: "/p2p/config",
  register: "/p2p/devices/register",
  presence: "/p2p/devices/presence",
  resolve: "/p2p/devices/resolve",
  request: "/p2p/pairings/request",
  pending: "/p2p/pairings/pending",
  result: "/p2p/pairings/result",
  respond: "/p2p/pairings/respond",
  revoke: "/p2p/pairings/revoke",
  signal: "/p2p/signal",
  relay: "/p2p/relay",
} as const;
export type Role = "host" | "client";
export type ObjectValue = Record<string, unknown>;
export interface Identity {
  id: string;
  connectionId: string;
  name: string;
  private: string;
  public: string;
  token: string;
  service: string;
}
export interface Device {
  id: string;
  connectionId: string;
  name: string;
  public: string;
  host: boolean;
  seen: number;
}
export interface Pairing {
  id: string;
  host: string;
  client: string;
  name: string;
  public: string;
  envelope: string;
  reply?: string;
  state: "pending" | "approved" | "denied";
  expires: number;
}
export interface Limits {
  maxHostDevices: number | null;
  maxAgentsPerDevice: number | null;
  maxConcurrentConnections: number | null;
  allowRelay: boolean;
}
export const unlimited: Limits = {
  maxHostDevices: null,
  maxAgentsPerDevice: null,
  maxConcurrentConnections: null,
  allowRelay: true,
};
export interface Decision {
  limits: Limits;
  products: string[] | null;
  quotaKey: string;
  revision: string;
}
export interface Grant {
  aud: "reign-connect-v1";
  namespace: string;
  hostId: string;
  deviceId: string;
  role: Role;
  scopes: string[];
  products: string[] | null;
  iat: number;
  exp: number;
  jti: string;
}
export interface Frame {
  peer?: string;
  device?: string;
  kind: string;
  body?: string;
  grant?: string;
  allowRelay?: boolean;
  protocol?: number;
  error?: string;
}
export interface ServiceConfig {
  protocol: number;
  deployment: "self-hosted" | "saas";
  accountRequired: boolean;
  loginProviders: string[];
  publicStun: string[];
  selfStun: string[];
  tcpStun: string[];
  grantPublicKey: string;
  serviceId: string;
}
export interface Provider {
  id: string;
  name: string;
  state: string;
  detail: string;
  installed: boolean;
  running: boolean;
  controllable: boolean;
  capabilities: Record<string, boolean>;
  model?: string;
}
export interface Session {
  id: string;
  providerId: string;
  title: string;
  preview: string;
  cwd: string;
  updatedAt: number;
  status: string;
  model?: string;
  activeTurnId?: string;
}
export interface Attachment {
  id: string;
  name: string;
  kind: string;
  mime: string;
  size: number;
  state: string;
  sha256?: string;
  thumbnail?: boolean;
  preview?: boolean;
}
export interface Item {
  id: string;
  role: string;
  type: string;
  text: string;
  status?: string;
  timestampMs?: number;
  attachments?: Attachment[];
  activities?: {
    kind: string;
    title: string;
    detail?: string;
    status?: string;
  }[];
}
export interface RPCRequest {
  id?: string;
  method: string;
  provider?: string;
  session?: string;
  [key: string]: unknown;
}
export function productFor(provider: string): string {
  return provider.startsWith("codex") ? "codex" : provider.split(/[.:/]/)[0];
}
export class APIError extends Error {
  constructor(
    public code: string,
    message: string,
    public status = 400,
    public details?: ObjectValue,
  ) {
    super(message);
    this.name = "APIError";
  }
}
export function validLimits(input: unknown): input is Limits {
  if (!input || typeof input !== "object") return false;
  const x = input as Limits;
  return (
    [x.maxHostDevices, x.maxAgentsPerDevice, x.maxConcurrentConnections].every(
      (v) => v === null || (Number.isSafeInteger(v) && v >= 0),
    ) && typeof x.allowRelay === "boolean"
  );
}
