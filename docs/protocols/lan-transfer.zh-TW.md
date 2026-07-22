# 區網傳輸協議

[English](lan-transfer.md)

LAN 傳輸使用 TLS 連線與 `/v1/transfers/...` 分段介面。預設分段為 8 MiB；每段及完整檔案都驗證 SHA-256。狀態、完成與重送以 transfer/file/chunk 識別，接收者只接受已授權目標。LAN 內容不經 Node 中繼，失敗可依路徑政策等待 LAN 或改用 Node。
