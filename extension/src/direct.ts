import { normalizeNodeURL, nodeURL, SharePayload } from "./protocol.js";

type TokenPair = { accessToken: string; refreshToken: string };
export type DirectDevice = { id: string; displayName: string; publicKey?: string; trustStatus: "PENDING" | "TRUSTED" | "REVOKED" };
export type DirectStatus = { connected: boolean; pending: boolean; nodeURL: string; devices: DirectDevice[] };

const mediaType = "application/vnd.nexdrop.v1+json";
const tokenKey = "directTokens";
const deviceKey = "directDeviceId";
const keyPairKey = "directDeviceKey";

export class DirectError extends Error {
  constructor(code: string, public readonly retryAfterSeconds?: number) { super(code); }
}

export async function pairExtension(node: string, identifier: string, password: string, totp: string, displayName: string) {
  const origin = normalizeNodeURL(node);
  const granted = await chrome.permissions.request({ origins: [`${origin}/*`] });
  if (!granted) throw new Error("PERMISSION_DENIED");
  const tokens = await raw<TokenPair>(origin, "/api/auth/login", { method: "POST", body: JSON.stringify({ identifier, password, totp }) });
  await chrome.storage.local.set({ [tokenKey]: tokens });
  await chrome.storage.sync.set({ nodeURL: origin });
  const user = await request<{ id: string }>("/api/account");
  const keys = await ensureDeviceKey();
  const stored = await chrome.storage.local.get(deviceKey);
  let id = String(stored[deviceKey] ?? "");
  const devices = await request<DirectDevice[]>("/api/devices");
  if (!id || !devices.some((item) => item.id === id && item.trustStatus !== "REVOKED")) {
    const created = await request<DirectDevice>("/api/devices", {
      method: "POST",
      body: JSON.stringify({ displayName: displayName.trim(), type: navigator.userAgent.includes("Edg/") ? "WEB_EDGE" : "WEB_CHROME", publicKey: keys.publicKey, keyAlgorithm: "X25519" }),
    });
    id = created.id;
    await chrome.storage.local.set({ [deviceKey]: id, directUserId: user.id });
  }
  const status = await directStatus();
  if (!status) throw new Error("PAIR_FAILED");
  return status;
}

export async function disconnectExtension() {
  await chrome.storage.local.remove([tokenKey, deviceKey, keyPairKey, "directUserId"]);
}

export async function directStatus(): Promise<DirectStatus | null> {
  const stored = await chrome.storage.local.get([tokenKey, deviceKey]);
  if (!stored[tokenKey] || !stored[deviceKey]) return null;
  const devices = await request<DirectDevice[]>("/api/devices");
  const own = devices.find((item) => item.id === stored[deviceKey]);
  if (!own || own.trustStatus === "REVOKED") return null;
  if (own.trustStatus === "TRUSTED") await attachSession(own.id);
  return {
    connected: own.trustStatus === "TRUSTED",
    pending: own.trustStatus === "PENDING",
    nodeURL: await nodeURL(),
    devices: devices.filter((item) => item.id !== own.id && item.trustStatus === "TRUSTED" && item.publicKey),
  };
}

export async function sendDirect(payload: SharePayload) {
  const targetIDs = payload.targetDeviceIds ?? [];
  if (!targetIDs.length) throw new Error("TARGET_REQUIRED");
  const devices = await request<DirectDevice[]>("/api/devices");
  const recipients = devices.filter((item) => targetIDs.includes(item.id) && item.trustStatus === "TRUSTED" && item.publicKey)
    .map((item) => ({ id: item.id, publicKey: item.publicKey! }));
  if (recipients.length !== targetIDs.length) throw new Error("TARGET_UNAVAILABLE");
  const plaintext = payload.text?.trim() || payload.url || payload.title || "";
  if (!plaintext) throw new Error("CONTENT_REQUIRED");
  const encrypted = await encryptText(plaintext, recipients);
  return request("/api/transfers", {
    method: "POST",
    body: JSON.stringify({
      targetType: targetIDs.length === 1 ? "SINGLE_DEVICE" : "MULTIPLE_DEVICES",
      targetDeviceIds: targetIDs,
      contentType: /^https?:\/\//i.test(plaintext) ? "URL" : "TEXT",
      routeMode: "AUTOMATIC",
      allowLargeFileViaNode: true,
      content: encrypted.content,
      wrappedContentKeys: encrypted.wrappedContentKeys,
    }),
  });
}

async function attachSession(id: string) {
  const challenge = await request<{ id: string; sessionId: string; ephemeralPublicKey: string; nonce: string }>(`/api/devices/${id}/session-challenge`, { method: "POST" });
  const keys = await ensureDeviceKey();
  const ephemeral = await crypto.subtle.importKey("raw", fromBase64(challenge.ephemeralPublicKey), { name: "X25519" }, false, []);
  const shared = await crypto.subtle.deriveBits({ name: "X25519", public: ephemeral }, keys.privateKey, 256);
  const key = await crypto.subtle.importKey("raw", shared, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const nonce = fromBase64(challenge.nonce);
  const prefix = new TextEncoder().encode(`nexdrop/session-attach/v1${challenge.sessionId}`);
  const message = new Uint8Array(prefix.length + nonce.length);
  message.set(prefix); message.set(nonce, prefix.length);
  const proof = toBase64(new Uint8Array(await crypto.subtle.sign("HMAC", key, message)));
  await request(`/api/devices/${id}/attach-session`, { method: "POST", body: JSON.stringify({ challengeId: challenge.id, proof }) });
}

async function request<T = unknown>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
  const origin = await nodeURL();
  const stored = await chrome.storage.local.get(tokenKey);
  const tokens = stored[tokenKey] as TokenPair | undefined;
  if (!tokens) throw new Error("NOT_PAIRED");
  try {
    return await raw<T>(origin, path, init, tokens.accessToken);
  } catch (error) {
    if (retry && error instanceof Error && error.message === "INVALID_TOKEN" && await refresh(origin, tokens.refreshToken)) return request<T>(path, init, false);
    throw error;
  }
}

async function refresh(origin: string, refreshToken: string) {
  try {
    const tokens = await raw<TokenPair>(origin, "/api/auth/refresh", { method: "POST", body: JSON.stringify({ refreshToken }) });
    await chrome.storage.local.set({ [tokenKey]: tokens });
    return true;
  } catch {
    await disconnectExtension();
    return false;
  }
}

async function raw<T>(origin: string, path: string, init: RequestInit, accessToken = ""): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", mediaType);
  if (init.body) headers.set("Content-Type", "application/json");
  if (accessToken) headers.set("Authorization", `Bearer ${accessToken}`);
  if (init.method && init.method !== "GET") headers.set("Idempotency-Key", crypto.randomUUID());
  const response = await fetch(`${origin}${path}`, { ...init, headers });
  if (!response.ok) {
    const body = await response.json().catch(() => ({})) as { error?: string | { code?: string } };
    const retryAfter = Number.parseInt(response.headers.get("Retry-After") ?? "", 10);
    throw new DirectError(typeof body.error === "string" ? body.error : body.error?.code ?? `HTTP_${response.status}`, Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : undefined);
  }
  return response.status === 204 ? undefined as T : response.json() as Promise<T>;
}

async function ensureDeviceKey() {
  const stored = await chrome.storage.local.get(keyPairKey);
  const value = stored[keyPairKey] as { privateKey: JsonWebKey; publicKey: string } | undefined;
  if (value) return { privateKey: await crypto.subtle.importKey("jwk", value.privateKey, { name: "X25519" }, false, ["deriveBits"]), publicKey: value.publicKey };
  const pair = await crypto.subtle.generateKey({ name: "X25519" }, true, ["deriveBits"]) as CryptoKeyPair;
  const created = { privateKey: await crypto.subtle.exportKey("jwk", pair.privateKey), publicKey: toBase64(new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey))) };
  await chrome.storage.local.set({ [keyPairKey]: created });
  return { privateKey: pair.privateKey, publicKey: created.publicKey };
}

async function encryptText(plaintext: string, recipients: Array<{ id: string; publicKey: string }>) {
  const contentKey = crypto.getRandomValues(new Uint8Array(32));
  const key = await crypto.subtle.importKey("raw", contentKey, "AES-GCM", false, ["encrypt"]);
  const nonce = crypto.getRandomValues(new Uint8Array(12));
  const ciphertext = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, key, new TextEncoder().encode(plaintext)));
  const content = toBase64(new TextEncoder().encode(JSON.stringify({ version: 1, nonce: toBase64(nonce), ciphertext: toBase64(ciphertext) })));
  const wrappedContentKeys: Record<string, string> = {};
  for (const recipient of recipients) wrappedContentKeys[recipient.id] = await wrapKey(contentKey, recipient.publicKey);
  return { content, wrappedContentKeys };
}

async function wrapKey(contentKey: Uint8Array, publicKey: string) {
  const recipient = await crypto.subtle.importKey("raw", fromBase64(publicKey), { name: "X25519" }, false, []);
  const ephemeral = await crypto.subtle.generateKey({ name: "X25519" }, true, ["deriveBits"]) as CryptoKeyPair;
  const shared = await crypto.subtle.deriveBits({ name: "X25519", public: recipient }, ephemeral.privateKey, 256);
  const source = await crypto.subtle.importKey("raw", shared, "HKDF", false, ["deriveKey"]);
  const wrappingKey = await crypto.subtle.deriveKey({ name: "HKDF", hash: "SHA-256", salt: new Uint8Array(32), info: new TextEncoder().encode("nexdrop/private-transfer/v1") }, source, { name: "AES-GCM", length: 256 }, false, ["encrypt"]);
  const nonce = crypto.getRandomValues(new Uint8Array(12));
  const ciphertext = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, wrappingKey, Uint8Array.from(contentKey)));
  const envelope = { version: 1, ephemeralPublicKey: toBase64(new Uint8Array(await crypto.subtle.exportKey("raw", ephemeral.publicKey))), nonce: toBase64(nonce), ciphertext: toBase64(ciphertext) };
  return toBase64(new TextEncoder().encode(JSON.stringify(envelope)));
}

function toBase64(value: Uint8Array) { let binary = ""; value.forEach((byte) => { binary += String.fromCharCode(byte); }); return btoa(binary); }
function fromBase64(value: string) { const binary = atob(value); return Uint8Array.from(binary, (character) => character.charCodeAt(0)); }
