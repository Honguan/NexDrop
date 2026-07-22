# LAN transfer protocol

[繁體中文](lan-transfer.zh-TW.md)

LAN transfer uses TLS connections and the `/v1/transfers/...` chunk interface. The default chunk is 8 MiB, and both every chunk and the complete file are verified with SHA-256. Transfer, file, and chunk identifiers govern state, completion, and retries. Receivers accept content only for authorized targets. LAN content does not pass through the Node; route policy can wait for LAN or fall back to the Node after failure.
