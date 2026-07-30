import { retryAfterSeconds } from "./errors";

export type User = {
  id: string;
  username: string;
  email: string;
  admin: boolean;
  totpEnabled: boolean;
};

export type Device = {
  id: string;
  displayName: string;
  type: string;
  publicKey?: string;
  keyAlgorithm?: string;
  trustStatus: "PENDING" | "TRUSTED" | "REVOKED";
  online: boolean;
  lastSeenAt?: string;
  createdAt: string;
};

export type Group = {
  id: string;
  name: string;
  ownerUserId: string;
  role: "OWNER" | "ADMIN" | "MEMBER";
  createdAt: string;
};

export type GroupDetails = Group & {
  members: Array<{
    userId: string;
    username: string;
    role: "OWNER" | "ADMIN" | "MEMBER";
    joinedAt: string;
  }>;
  devices: Array<{
    id: string;
    ownerUserId: string;
    displayName: string;
    type: string;
    publicKey: string;
    keyAlgorithm: string;
    addedAt: string;
  }>;
};

export type TransferTarget = {
  deviceId: string;
  selectedRoute: string;
  status: string;
  bytesTransferred: number;
};

export type Transfer = {
  id: string;
  senderUserId: string;
  senderDeviceId?: string;
  targetType: string;
  groupId?: string;
  contentType: string;
  content?: string;
  wrappedContentKeys?: Record<string, string>;
  files: Array<{ id: string; name: string; mimeType: string; size: number; sha256: string; chunkSize: number; chunkCount: number }>;
  targets: TransferTarget[];
  status: string;
  createdAt: string;
  expiresAt: string;
};

export type Overview = {
  transferCount: number;
  totalBytes: number;
  succeeded: number;
  failed: number;
  routeCounts: Record<string, number>;
  routeBytes: Record<string, number>;
};

export type DailyTransfer = {
  date: string;
  count: number;
  totalBytes: number;
  lanBytes: number;
  nodeBytes: number;
  failed: number;
};

export type DeviceStatistic = {
  deviceId: string;
  displayName: string;
  deviceType: string;
  trustStatus: "PENDING" | "TRUSTED" | "REVOKED";
  online: boolean;
  lastSeenAt?: string;
  sentCount: number;
  receivedCount: number;
  sentBytes: number;
  receivedBytes: number;
  averageBytesPerSecond: number;
};

export type GroupStatistic = {
  groupId: string;
  name: string;
  messageCount: number;
  fileCount: number;
  transferBytes: number;
  activeDevices: number;
  activeUsers: number;
};

type TokenPair = {
  accessToken: string;
  refreshToken: string;
  accessExpiresAt: string;
  refreshExpiresAt: string;
};

export type NodeCapabilityDocument = {
  nodeIdentity: string;
  versionFingerprint: string;
  protocolVersion: string;
  capabilities: string[];
  limits: {
    maxChunkSize: number;
    maxParallelChunks: number;
    maxRecipients: number;
  };
};

export class APIError extends Error {
  constructor(
    public readonly code: string,
    public readonly status: number,
    public readonly retryAfterSeconds?: number,
    public readonly details: Record<string, unknown> = {},
  ) {
    super(code);
  }
}

const tokenKey = "nexdrop.tokens.v1";
const nodeKeyStorage = "nexdrop.node_key.v2";
const capabilityStorage = "nexdrop.node_capabilities.v1";
const versionMediaType = "application/vnd.nexdrop.v1+json";
const supportedCapabilities = [
  "capability_negotiation",
  "structured_errors",
  "cursor_pagination",
  "idempotency_replay",
  "resumable_chunks",
  "realtime_versions",
] as const;
const supportedProtocols = new Set(["1.0", "1.1", "1.2"]);

class APIClient {
  private tokens: TokenPair | null = this.readTokens();
  private refreshing: Promise<boolean> | null = null;
  private capabilityDocument: NodeCapabilityDocument | null = null;

  nodeKey() { return localStorage.getItem(nodeKeyStorage) ?? ""; }

  setNodeKey(value: string) { localStorage.setItem(nodeKeyStorage, value.trim()); }

  hasSession() {
    return this.tokens !== null;
  }

  webSocketURL() {
    if (!this.tokens) return null;
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const nodeProtocol = compatibleProtocol(this.capabilityDocument);
    const query = new URLSearchParams({
      access_token: this.tokens.accessToken,
      protocolVersion: nodeProtocol,
      clientVersion: `web-v${nodeProtocol}`,
    });
    if (this.supports("capability_negotiation")) {
      query.set("capabilities", supportedCapabilities.join(","));
    }
    return `${protocol}//${location.host}/ws?${query}`;
  }

  async login(identifier: string, password: string, totp = "") {
    await this.refreshCapabilities().catch(() => undefined);
    const response = await fetch("/api/auth/login", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: versionMediaType,
        "X-NexDrop-Capabilities": supportedCapabilities.join(","),
      },
      body: JSON.stringify({ identifier, password, totp }),
    });
    if (!response.ok) throw await this.error(response);
    this.saveTokens((await response.json()) as TokenPair);
  }

  async refreshCapabilities() {
    this.capabilityDocument = null;
    const response = await fetch("/api/version", {
      headers: {
        Accept: versionMediaType,
        "X-NexDrop-Capabilities": supportedCapabilities.join(","),
      },
    });
    if (!response.ok) throw await this.error(response);
    const raw = (await response.json()) as Record<string, unknown>;
    const document = parseCapabilityDocument(raw, location.origin);
    this.capabilityDocument = document;
    localStorage.setItem(capabilityStorage, JSON.stringify(document));
    return document;
  }

  cachedCapabilities() {
    try {
      const value = localStorage.getItem(capabilityStorage);
      return value ? parseCapabilityDocument(JSON.parse(value) as Record<string, unknown>, location.origin) : null;
    } catch {
      return null;
    }
  }

  supports(capability: string) {
    return supportedCapabilities.includes(capability as typeof supportedCapabilities[number])
      && Boolean(this.capabilityDocument?.capabilities.includes(capability));
  }

  async logout() {
    const refreshToken = this.tokens?.refreshToken;
    this.clearTokens();
    if (!refreshToken) return;
    await fetch("/api/auth/logout", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: versionMediaType,
        "X-NexDrop-Capabilities": supportedCapabilities.join(","),
      },
      body: JSON.stringify({ refreshToken }),
    });
  }


  async get<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: "GET" });
  }

  async send<T>(path: string, method: string, body?: unknown): Promise<T> {
    return this.request<T>(path, {
      method,
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  }

  async uploadChunk(path: string, body: ArrayBuffer, sha256: string) {
    await this.requestRaw(path, { method: "POST", headers: { "X-Chunk-SHA256": sha256 }, body });
  }

  async downloadChunk(path: string) {
    const response = await this.requestRaw(path, { method: "GET" });
    return response.arrayBuffer();
  }

  private async request<T>(path: string, init: RequestInit, retry = true): Promise<T> {
    const response = await this.requestRaw(path, init, retry);
    if (response.status === 204) return undefined as T;
    return (await response.json()) as T;
  }

  private async requestRaw(path: string, init: RequestInit, retry = true): Promise<Response> {
    const headers = new Headers(init.headers);
    headers.set("Accept", versionMediaType);
    headers.set("X-NexDrop-Capabilities", supportedCapabilities.join(","));
    if (init.method !== "GET" && !headers.has("Idempotency-Key")) {
      headers.set("Idempotency-Key", crypto.randomUUID());
    }
    if (this.tokens) headers.set("Authorization", `Bearer ${this.tokens.accessToken}`);
    const nodeKey = this.nodeKey();
    if (nodeKey) headers.set("X-NexDrop-Node-Key", nodeKey);
    const response = await fetch(path, { ...init, headers });
    if (response.status === 401 && retry && (await this.refresh())) {
      return this.requestRaw(path, { ...init, headers }, false);
    }
    if (!response.ok) throw await this.error(response);
    return response;
  }

  private async refresh() {
    if (!this.tokens) return false;
    if (this.refreshing) return this.refreshing;
    this.refreshing = (async () => {
      try {
        const response = await fetch("/api/auth/refresh", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-NexDrop-Capabilities": supportedCapabilities.join(","),
          },
          body: JSON.stringify({ refreshToken: this.tokens?.refreshToken }),
        });
        if (!response.ok) throw new Error("refresh failed");
        this.saveTokens((await response.json()) as TokenPair);
        return true;
      } catch {
        this.clearTokens();
        return false;
      } finally {
        this.refreshing = null;
      }
    })();
    return this.refreshing;
  }

  private async error(response: Response) {
    const body = (await response.json().catch(() => ({}))) as {
      error?: string | { code?: string; details?: Record<string, unknown> };
    };
    const code = typeof body.error === "string" ? body.error : body.error?.code;
    return new APIError(
      code ?? "INTERNAL_ERROR",
      response.status,
      retryAfterSeconds(response.headers.get("Retry-After")),
      typeof body.error === "object" ? body.error.details : undefined,
    );
  }

  private readTokens(): TokenPair | null {
    try {
      const value = localStorage.getItem(tokenKey);
      return value ? (JSON.parse(value) as TokenPair) : null;
    } catch {
      return null;
    }
  }

  private saveTokens(tokens: TokenPair) {
    this.tokens = tokens;
    localStorage.setItem(tokenKey, JSON.stringify(tokens));
  }

  private clearTokens() {
    this.tokens = null;
    localStorage.removeItem(tokenKey);
  }
}

export const api = new APIClient();

export function compatibleProtocol(document: NodeCapabilityDocument | null) {
  return document && supportedProtocols.has(document.protocolVersion)
    ? document.protocolVersion
    : "1.0";
}

export function parseCapabilityDocument(
  raw: Record<string, unknown>,
  fallbackNodeIdentity: string,
): NodeCapabilityDocument {
  const limits = typeof raw.limits === "object" && raw.limits !== null
    ? raw.limits as Record<string, unknown>
    : {};
  const capabilities = Array.isArray(raw.capabilities)
    ? raw.capabilities.filter((value): value is string => typeof value === "string")
    : [];
  return {
    nodeIdentity: typeof raw.nodeIdentity === "string" ? raw.nodeIdentity : fallbackNodeIdentity,
    versionFingerprint: typeof raw.versionFingerprint === "string"
      ? raw.versionFingerprint
      : [raw.productVersion, raw.buildCommit, raw.protocolVersion].join("|"),
    protocolVersion: typeof raw.protocolVersion === "string" ? raw.protocolVersion : "1.0",
    capabilities,
    limits: {
      maxChunkSize: typeof limits.maxChunkSize === "number" ? limits.maxChunkSize : 8 * 1024 * 1024,
      maxParallelChunks: typeof limits.maxParallelChunks === "number" ? limits.maxParallelChunks : 3,
      maxRecipients: typeof limits.maxRecipients === "number" ? limits.maxRecipients : 100,
    },
  };
}

export function statisticsPath(path: string, days = 7) {
  const to = new Date();
  const from = new Date(to.getTime() - days * 24 * 60 * 60 * 1000);
  const query = new URLSearchParams({ from: from.toISOString(), to: to.toISOString() });
  return `${path}?${query}`;
}
