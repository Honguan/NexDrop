export const nativeHost = "com.nexdrop.bridge";

export type SharePayload = {
  kind: "PAGE" | "LINK" | "SELECTION" | "IMAGE";
  title?: string;
  url?: string;
  text?: string;
  targetDeviceIds?: string[];
  groupId?: string;
};

export type NativeRequest =
  | { id: string; type: "status" }
  | { id: string; type: "share"; payload: SharePayload };

export type NativeResponse = {
  id: string;
  ok: boolean;
  error?: string;
  status?: {
    connected: boolean;
    nodeURL: string;
    devices: Array<{ id: string; name: string; online: boolean }>;
    groups: Array<{ id: string; name: string }>;
  };
};

export function requestNative(request: NativeRequest): Promise<NativeResponse> {
  return new Promise((resolve, reject) => {
    chrome.runtime.sendNativeMessage(nativeHost, request, (response?: NativeResponse) => {
      if (chrome.runtime.lastError) {
        reject(new Error(chrome.runtime.lastError.message));
        return;
      }
      if (!response) {
        reject(new Error("EMPTY_NATIVE_RESPONSE"));
        return;
      }
      resolve(response);
    });
  });
}

export function requestID() {
  return crypto.randomUUID();
}

export async function nodeURL() {
  const stored = await chrome.storage.sync.get({ nodeURL: "https://localhost" });
  return normalizeNodeURL(String(stored.nodeURL));
}

export function normalizeNodeURL(value: string) {
  const url = new URL(value.trim());
  if (url.protocol !== "https:" && !(url.protocol === "http:" && ["127.0.0.1", "localhost"].includes(url.hostname))) {
    throw new Error("INVALID_NODE_URL");
  }
  return url.origin;
}

export async function openWebShare(payload: SharePayload) {
  const encoded = bytesToBase64URL(new TextEncoder().encode(JSON.stringify(payload)));
  await chrome.tabs.create({ url: `${await nodeURL()}/#share=${encoded}` });
}

function bytesToBase64URL(value: Uint8Array) {
  let binary = "";
  for (const byte of value) binary += String.fromCharCode(byte);
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}
