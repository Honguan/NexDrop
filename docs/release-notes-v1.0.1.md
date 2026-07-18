# NexDrop 1.0.1

此修補版改善 Linux Node 的首次安裝、設定更新與版本升級流程，並修正 LAN 傳輸相關的高風險 CodeQL 告警。

安裝程式現在會先說明條件、產生安全隨機預設，再讓操作者接受、修改或重新產生；啟動 Node 前也會驗證 PostgreSQL 密碼是否與既有資料 volume 一致。既有部署可執行 `./deploy/nexdrop update 1.0.1`，更新前會建立備份並保留 `.env` 與游標秘密。若更新失敗，Node 會停止且不會在未知 migration 狀態下自動降級啟動。

Web 與 Flutter 用戶端會依 `Retry-After` 顯示實際等待秒數。API v1 的 `RATE_LIMITED` 訊息也會明確要求依該標頭等待後再試。

安全修正包含限制 LAN 身分資料與傳輸配置輸入大小，以及改用系統信任庫驗證 TLS 憑證。完整變更見 [CHANGELOG](https://github.com/Honguan/NexDrop/blob/v1.0.1/CHANGELOG.md)。

相容性：API v1、協議 1.1，最低用戶端 1.0。正式產物附有 `checksums-sha256.txt`、SPDX SBOM、Artifact Attestation 與 GHCR Cosign 簽章。

安裝警告：若未配置 Android keystore，APK 為未簽章產物，裝置可能要求允許未知來源；若未配置 Windows 憑證，EXE 與 ZIP 可能顯示 SmartScreen 警告。這些選填簽章材料不影響本版本發布。
