const databaseName = "nexdrop-keys";
const storeName = "device-keys";

type StoredKeyPair = {
  privateKey: CryptoKey;
  publicKey: string;
};

type WrappedKey = {
  version: number;
  ephemeralPublicKey: string;
  nonce: string;
  ciphertext: string;
};

type ContentEnvelope = {
  version: number;
  nonce: string;
  ciphertext: string;
};

export type EncryptedFileUpload = {
  metadata: { name: string; mimeType: string; size: number };
  chunks: Array<{ data: ArrayBuffer; sha256: string }>;
  record: { name: string; mimeType: string; size: number; sha256: string; chunkSize: number; chunkCount: number };
};

export async function ensureDeviceKey(userID: string): Promise<StoredKeyPair> {
  const existing = await readKey(userID);
  if (existing) return existing;
  const pair = (await crypto.subtle.generateKey(
    { name: "X25519" },
    false,
    ["deriveBits"],
  )) as CryptoKeyPair;
  const value = {
    privateKey: pair.privateKey,
    publicKey: toBase64(new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey))),
  };
  await writeKey(userID, value);
  return value;
}

export function deviceID(userID: string) {
  return localStorage.getItem(`nexdrop.device.${userID}`);
}

export function rememberDevice(userID: string, id: string) {
  localStorage.setItem(`nexdrop.device.${userID}`, id);
}

export async function encryptText(
  plaintext: string,
  recipients: Array<{ id: string; publicKey: string }>,
) {
  const contentKeyBytes = crypto.getRandomValues(new Uint8Array(32));
  return {
    content: await encryptEnvelope(new TextEncoder().encode(plaintext), contentKeyBytes),
    wrappedContentKeys: await wrapForRecipients(contentKeyBytes, recipients),
  };
}

export async function encryptFiles(files: File[], recipients: Array<{ id: string; publicKey: string }>) {
  const plaintextChunkSize = 8 * 1024 * 1024;
  const encryptedFiles: EncryptedFileUpload[] = [];
  const contentKeyBytes = crypto.getRandomValues(new Uint8Array(32));
  const contentKey = await crypto.subtle.importKey("raw", contentKeyBytes, "AES-GCM", false, ["encrypt"]);
  for (const [fileIndex, file] of files.entries()) {
    const chunks: EncryptedFileUpload["chunks"] = [];
    const encryptedParts: Uint8Array[] = [];
    for (let offset = 0; offset < file.size; offset += plaintextChunkSize) {
      const nonce = crypto.getRandomValues(new Uint8Array(12));
      const plaintext = await file.slice(offset, Math.min(offset + plaintextChunkSize, file.size)).arrayBuffer();
      const ciphertext = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, contentKey, plaintext));
      const encrypted = new Uint8Array(nonce.length + ciphertext.length);
      encrypted.set(nonce);
      encrypted.set(ciphertext, nonce.length);
      encryptedParts.push(encrypted);
      chunks.push({ data: encrypted.buffer, sha256: toHex(new Uint8Array(await crypto.subtle.digest("SHA-256", encrypted))) });
    }
    const totalSize = encryptedParts.reduce((total, part) => total + part.length, 0);
    const complete = new Uint8Array(totalSize);
    let position = 0;
    encryptedParts.forEach((part) => { complete.set(part, position); position += part.length; });
    encryptedFiles.push({
      metadata: { name: file.name, mimeType: file.type || "application/octet-stream", size: file.size },
      chunks,
      record: {
        name: `encrypted-${fileIndex}.nxd`,
        mimeType: "application/octet-stream",
        size: totalSize,
        sha256: toBase64(new Uint8Array(await crypto.subtle.digest("SHA-256", complete))),
        chunkSize: plaintextChunkSize + 28,
        chunkCount: chunks.length,
      },
    });
  }
  const metadata = encryptedFiles.map((item) => item.metadata);
  return {
    content: await encryptEnvelope(new TextEncoder().encode(JSON.stringify(metadata)), contentKeyBytes),
    wrappedContentKeys: await wrapForRecipients(contentKeyBytes, recipients),
    files: encryptedFiles,
  };
}

export async function decryptText(
  userID: string,
  encryptedContent: string,
  wrappedValue: string,
) {
  return new TextDecoder().decode(await decryptEnvelope(userID, encryptedContent, wrappedValue));
}

export async function decryptFileChunks(userID: string, wrappedValue: string, chunks: ArrayBuffer[]) {
  const contentKeyBytes = await unwrapKey(userID, wrappedValue);
  const contentKey = await crypto.subtle.importKey("raw", contentKeyBytes, "AES-GCM", false, ["decrypt"]);
  const plaintext: ArrayBuffer[] = [];
  for (const chunk of chunks) {
    const bytes = new Uint8Array(chunk);
    if (bytes.length < 28) throw new Error("INVALID_ENCRYPTED_FILE");
    plaintext.push(await crypto.subtle.decrypt({ name: "AES-GCM", iv: bytes.slice(0, 12) }, contentKey, bytes.slice(12)));
  }
  return plaintext;
}

async function encryptEnvelope(plaintext: Uint8Array, contentKeyBytes: Uint8Array) {
  const contentKey = await crypto.subtle.importKey("raw", Uint8Array.from(contentKeyBytes), "AES-GCM", false, ["encrypt"]);
  const contentNonce = crypto.getRandomValues(new Uint8Array(12));
  const ciphertext = await crypto.subtle.encrypt({ name: "AES-GCM", iv: contentNonce }, contentKey, Uint8Array.from(plaintext));
  const content: ContentEnvelope = { version: 1, nonce: toBase64(contentNonce), ciphertext: toBase64(new Uint8Array(ciphertext)) };
  return toBase64(new TextEncoder().encode(JSON.stringify(content)));
}

async function decryptEnvelope(userID: string, encryptedContent: string, wrappedValue: string) {
  const contentKeyBytes = await unwrapKey(userID, wrappedValue);
  const contentKey = await crypto.subtle.importKey("raw", contentKeyBytes, "AES-GCM", false, ["decrypt"]);
  const content = JSON.parse(new TextDecoder().decode(fromBase64(encryptedContent))) as ContentEnvelope;
  return new Uint8Array(await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: fromBase64(content.nonce) },
    contentKey,
    fromBase64(content.ciphertext),
  ));
}

async function unwrapKey(userID: string, wrappedValue: string) {
  const stored = await readKey(userID);
  if (!stored) throw new Error("DEVICE_KEY_UNAVAILABLE");
  const wrapped = JSON.parse(new TextDecoder().decode(fromBase64(wrappedValue))) as WrappedKey;
  const ephemeral = await crypto.subtle.importKey("raw", fromBase64(wrapped.ephemeralPublicKey), { name: "X25519" }, false, []);
  const wrappingKey = await deriveWrappingKey(stored.privateKey, ephemeral, ["decrypt"]);
  return crypto.subtle.decrypt({ name: "AES-GCM", iv: fromBase64(wrapped.nonce) }, wrappingKey, fromBase64(wrapped.ciphertext));
}

async function wrapForRecipients(contentKeyBytes: Uint8Array, recipients: Array<{ id: string; publicKey: string }>) {
  const wrappedContentKeys: Record<string, string> = {};
  for (const recipient of recipients) {
    wrappedContentKeys[recipient.id] = toBase64(new TextEncoder().encode(JSON.stringify(await wrapKey(contentKeyBytes, recipient.publicKey))));
  }
  return wrappedContentKeys;
}

export async function proveDeviceSession(userID: string, ephemeralPublicKey: string, nonce: string, sessionID: string) {
  const stored = await readKey(userID);
  if (!stored) throw new Error("DEVICE_KEY_UNAVAILABLE");
  const ephemeral = await crypto.subtle.importKey("raw", fromBase64(ephemeralPublicKey), { name: "X25519" }, false, []);
  const shared = await crypto.subtle.deriveBits({ name: "X25519", public: ephemeral }, stored.privateKey, 256);
  const key = await crypto.subtle.importKey("raw", shared, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const prefix = new TextEncoder().encode(`nexdrop/session-attach/v1${sessionID}`);
  const message = new Uint8Array(prefix.length + fromBase64(nonce).length);
  message.set(prefix);
  message.set(fromBase64(nonce), prefix.length);
  return toBase64(new Uint8Array(await crypto.subtle.sign("HMAC", key, message)));
}

async function wrapKey(contentKey: Uint8Array, recipientPublicKey: string): Promise<WrappedKey> {
  const recipient = await crypto.subtle.importKey(
    "raw",
    fromBase64(recipientPublicKey),
    { name: "X25519" },
    false,
    [],
  );
  const ephemeral = (await crypto.subtle.generateKey(
    { name: "X25519" },
    true,
    ["deriveBits"],
  )) as CryptoKeyPair;
  const wrappingKey = await deriveWrappingKey(ephemeral.privateKey, recipient, ["encrypt"]);
  const nonce = crypto.getRandomValues(new Uint8Array(12));
  const ciphertext = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, wrappingKey, Uint8Array.from(contentKey));
  return {
    version: 1,
    ephemeralPublicKey: toBase64(new Uint8Array(await crypto.subtle.exportKey("raw", ephemeral.publicKey))),
    nonce: toBase64(nonce),
    ciphertext: toBase64(new Uint8Array(ciphertext)),
  };
}

async function deriveWrappingKey(privateKey: CryptoKey, publicKey: CryptoKey, usages: KeyUsage[]) {
  const shared = await crypto.subtle.deriveBits({ name: "X25519", public: publicKey }, privateKey, 256);
  const source = await crypto.subtle.importKey("raw", shared, "HKDF", false, ["deriveKey"]);
  return crypto.subtle.deriveKey(
    {
      name: "HKDF",
      hash: "SHA-256",
      salt: new Uint8Array(32),
      info: new TextEncoder().encode("nexdrop/private-transfer/v1"),
    },
    source,
    { name: "AES-GCM", length: 256 },
    false,
    usages,
  );
}

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(databaseName, 1);
    request.onupgradeneeded = () => request.result.createObjectStore(storeName);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

async function readKey(userID: string): Promise<StoredKeyPair | undefined> {
  const database = await openDatabase();
  return new Promise((resolve, reject) => {
    const transaction = database.transaction(storeName, "readonly");
    const request = transaction.objectStore(storeName).get(userID);
    request.onsuccess = () => resolve(request.result as StoredKeyPair | undefined);
    request.onerror = () => reject(request.error);
    transaction.oncomplete = () => database.close();
  });
}

async function writeKey(userID: string, value: StoredKeyPair) {
  const database = await openDatabase();
  return new Promise<void>((resolve, reject) => {
    const transaction = database.transaction(storeName, "readwrite");
    transaction.objectStore(storeName).put(value, userID);
    transaction.oncomplete = () => {
      database.close();
      resolve();
    };
    transaction.onerror = () => reject(transaction.error);
  });
}

function toBase64(value: Uint8Array) {
  let binary = "";
  for (const byte of value) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function fromBase64(value: string) {
  const binary = atob(value);
  const result = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) result[index] = binary.charCodeAt(index);
  return result;
}

function toHex(value: Uint8Array) {
  return Array.from(value, (byte) => byte.toString(16).padStart(2, "0")).join("");
}
