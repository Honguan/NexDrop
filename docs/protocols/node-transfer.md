# Node transfer protocol

[繁體中文](node-transfer.zh-TW.md)

The Node creates tasks, accepts encrypted chunks, completes assembly, and serves target downloads through HTTPS. Versioned mutations include an `Idempotency-Key`; chunk index and SHA-256 provide deduplication. The Node enforces quotas, expiration, and authorization. Insufficient disk space rejects new Node-hosted files without blocking text or direct LAN transfer.
