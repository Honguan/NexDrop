# 診斷包

[English](diagnostics.md)

在儲存庫目錄建立可安全提供支援人員的診斷包：

```sh
./deploy/nexdrop diagnostics --output diagnostics.zip
```

Docker 需要權限時，命令會自動使用管理員權限；ZIP 僅允許擁有者讀寫，且只執行唯讀檢查。它不會修復設定、建立資料庫紀錄或改變傳輸狀態。

壓縮檔包含具 schema 版本的 manifest、產品與 Commit 版本、執行環境、健康檢查及已遮蔽的有效設定。資料庫 URL 憑證、Bearer Token、密碼、Token、秘密、憑證及密鑰都會替換為 `[REDACTED]`。系統不會收集訊息內容、明文檔名、解密索引、使用者檔案、私鑰、節點密鑰或 TOTP 秘密。

分享前：

1. 傳輸時加密壓縮檔。
2. 確認 manifest 的版本與 Commit 正確。
3. 只提供診斷包，不提供 `.env`、資料庫備份、儲存目錄或完整容器日誌。
4. 事件保存期限結束後刪除本機支援副本。

遮蔽邊界詳見[診斷隱私規格](diagnostics-privacy.zh-TW.md)，穩定傳輸階段詳見[傳輸事件](transfer-events.zh-TW.md)。
