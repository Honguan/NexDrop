# Changelog

本檔案記錄 NexDrop 使用者可感知的變更，格式遵循 Keep a Changelog 與語意化版本。

## [Unreleased]

## [1.0.1] - 2026-07-18

### Added

- 互動式安裝精靈、安全隨機預設、`configure` 設定指令及可指定目標版本的備份式更新流程。

### Changed

- Web 與 Flutter 用戶端會依 `Retry-After` 顯示明確的限流等待時間，並改善無效請求訊息。
- Release 封裝流程會排除暫存簽章驗證檔案，並清楚標示未簽章平台產物的安裝警告。

### Security

- 限制 LAN 身分資料與傳輸配置的輸入大小，並以系統信任庫驗證 TLS 憑證，修正高風險 CodeQL 告警。

## [1.0.0] - 2026-07-16

### Added

- Windows、Android、Web、Chrome 與 Edge 的混合式多裝置傳輸。
- LAN 優先、Node 後援、分段續傳、SHA-256 驗證與端對端加密。
- 裝置、群組、配額、統計、稽核、備份、還原與管理介面。
- API v1 協商錯誤格式、request ID、冪等控制與游標式傳輸／管理歷史。
- 傳輸、失敗與稽核歷史游標以 UTC 建立時間、穩定 UUID 與 HMAC 簽章防止竄改。
- GitHub Actions 驗證、跨平台 Release、SBOM、雜湊與容器簽章流程。

### Security

- Refresh Token 輪替、管理員 TOTP、登入／配對／管理速率限制及結構化去敏日誌。

### Known Issues

- Android 正式建置需要配置 release keystore；Windows 正式建置需要程式碼簽章憑證。
- 自簽或內部 CA 的 Node 憑證需先安裝到用戶端系統信任庫。

[1.0.0]: https://github.com/Honguan/NexDrop/releases/tag/v1.0.0
[1.0.1]: https://github.com/Honguan/NexDrop/releases/tag/v1.0.1
