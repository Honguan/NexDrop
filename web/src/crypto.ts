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
  const contentKey = await crypto.subtle.importKey("raw", contentKeyBytes, "AES-GCM", false, ["encrypt"]);
  const contentNonce = crypto.getRandomValues(new Uint8Array(12));
  const ciphertext = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv: contentNonce },
    contentKey,
    new TextEncoder().encode(plaintext),
  );
  const content: ContentEnvelope = {
    version: 1,
    nonce: toBase64(contentNonce),
    ciphertext: toBase64(new Uint8Array(ciphertext)),
  };
  const wrappedContentKeys: Record<string, string> = {};
  for (const recipient of recipients) {
    wrappedContentKeys[recipient.id] = toBase64(
      new TextEncoder().encode(JSON.stringify(await wrapKey(contentKeyBytes, recipient.publicKey))),
    );
  }
  return {
    content: toBase64(new TextEncoder().encode(JSON.stringify(content))),
    wrappedContentKeys,
  };
}

export async function decryptText(
  userID: string,
  encryptedContent: string,
  wrappedValue: string,
) {
  const stored = await readKey(userID);
  if (!stored) throw new Error("DEVICE_KEY_UNAVAILABLE");
  const wrapped = JSON.parse(new TextDecoder().decode(fromBase64(wrappedValue))) as WrappedKey;
  const ephemeral = await crypto.subtle.importKey(
    "raw",
    fromBase64(wrapped.ephemeralPublicKey),
    { name: "X25519" },
    false,
    [],
  );
  const wrappingKey = await deriveWrappingKey(stored.privateKey, ephemeral, ["decrypt"]);
  const contentKeyBytes = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: fromBase64(wrapped.nonce) },
    wrappingKey,
    fromBase64(wrapped.ciphertext),
  );
  const contentKey = await crypto.subtle.importKey("raw", contentKeyBytes, "AES-GCM", false, ["decrypt"]);
  const content = JSON.parse(new TextDecoder().decode(fromBase64(encryptedContent))) as ContentEnvelope;
  const plaintext = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: fromBase64(content.nonce) },
    contentKey,
    fromBase64(content.ciphertext),
  );
  return new TextDecoder().decode(plaintext);
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
