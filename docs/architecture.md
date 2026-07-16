# 系統架構

NexDrop 採模組化單體。`cmd/nexdrop` 組裝 `internal/` 中的 auth、device、group、transfer、filetransfer、presence、analytics、admin、maintenance 與 monitoring 模組；模組透過明確 Store 介面存取 PostgreSQL。

```text
Windows / Android / Web / Extension
             │ HTTPS + WebSocket
             ▼
      NexDrop Node + Caddy ── PostgreSQL
             │
             └── 加密檔案分段儲存

裝置 A ◄──────── TLS LAN 直傳 ────────► 裝置 B
```

路徑選擇以 LAN 優先，無法直連時使用 Node；訊息與檔案內容在用戶端加密，Node 只保存密文與每設備包裝金鑰。背景 Worker 必須可重入，資料庫交易是任務與狀態的唯一真相來源。
