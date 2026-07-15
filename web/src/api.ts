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

export type AdminUser = User & { disabledAt?: string; createdAt: string };

export type StorageOverview = {
  fileCount: number;
  storedBytes: number;
  uploadingBytes: number;
  expiredBytes: number;
  quotaBytesUsed: number;
  quotaByteLimit: number;
};

export type NodeSettings = {
  singleFileLimitBytes: number;
  defaultUserQuotaBytes: number;
  defaultGroupQuotaBytes: number;
  nodeCacheLimitBytes: number;
  defaultUserDailyBytes: number;
  defaultGroupDailyBytes: number;
  diskWarningPercent: number;
  diskStopPercent: number;
};

export type AuditLog = {
  id: string;
  action: string;
  targetType: string;
  targetId?: string;
  createdAt: string;
};

type TokenPair = {
  accessToken: string;
  refreshToken: string;
  accessExpiresAt: string;
  refreshExpiresAt: string;
};

export class APIError extends Error {
  constructor(
    public readonly code: string,
    public readonly status: number,
  ) {
    super(code);
  }
}

const tokenKey = "nexdrop.tokens.v1";

class APIClient {
  private tokens: TokenPair | null = this.readTokens();
  private refreshing: Promise<boolean> | null = null;

  hasSession() {
    return this.tokens !== null;
  }

  webSocketURL() {
    if (!this.tokens) return null;
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const query = new URLSearchParams({
      access_token: this.tokens.accessToken,
      protocolVersion: "1.1",
      clientVersion: "web-v1.1",
    });
    return `${protocol}//${location.host}/ws?${query}`;
  }

  async login(identifier: string, password: string, totp = "") {
    const response = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ identifier, password, totp }),
    });
    if (!response.ok) throw await this.error(response);
    this.saveTokens((await response.json()) as TokenPair);
  }

  async logout() {
    const refreshToken = this.tokens?.refreshToken;
    this.clearTokens();
    if (!refreshToken) return;
    await fetch("/api/auth/logout", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
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
    if (this.tokens) headers.set("Authorization", `Bearer ${this.tokens.accessToken}`);
    const response = await fetch(path, { ...init, headers });
    if (response.status === 401 && retry && (await this.refresh())) {
      return this.requestRaw(path, init, false);
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
          headers: { "Content-Type": "application/json" },
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
    const body = (await response.json().catch(() => ({}))) as { error?: string };
    return new APIError(body.error ?? "INTERNAL_ERROR", response.status);
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

export function statisticsPath(path: string, days = 7) {
  const to = new Date();
  const from = new Date(to.getTime() - days * 24 * 60 * 60 * 1000);
  const query = new URLSearchParams({ from: from.toISOString(), to: to.toISOString() });
  return `${path}?${query}`;
}
