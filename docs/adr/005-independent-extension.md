# ADR-005: Independent browser-extension device

[繁體中文](005-independent-extension.zh-TW.md)

Status: Accepted

Context: Requiring a running Windows client made the extension unavailable on mobile or non-Windows browsers and blurred the identity of two separately installed clients.

Decision: Register each Chrome or Edge extension as an independent Web device. It authenticates directly to the configured HTTPS Node, keeps its own X25519 key and session in extension-local storage, and requests host access only for that Node. It does not require Native Messaging or a desktop process.

Consequences: Extension and desktop installations appear as separate devices and can be revoked independently. The extension cannot provide direct LAN file transfer and delegates full file workflows to NexDrop Web.
