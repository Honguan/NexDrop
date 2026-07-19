# 安全設計

- 密碼以 bcrypt 保存；Access Token 短效，Refresh Token 可撤銷且每次更新都輪替。
- 管理介面要求管理員身分、近期密碼驗證與 TOTP。
- Web 登入、節點密鑰加入與管理端點採固定窗口限制；超額回傳 429 與 `Retry-After`。
- 內容使用 AES-256-GCM，每台目標設備以 X25519/HKDF 包裝內容金鑰。
- Node 重新驗證所有權限、配額、檔名與檔案路徑；不信任用戶端角色欄位。
- JSON 日誌只含 UTC、層級、模組、request/transfer ID、狀態與錯誤碼，不記錄密碼、Token、私鑰或內容。
- 正式產物須通過 CodeQL、相依掃描、checksum、attestation 與簽章驗證。

威脅回報方式見根目錄 SECURITY 文件。Node 私鑰、設備私鑰、keystore、PFX、資料庫備份與 `.env` 不得進入 Workflow artifact 或快取。
