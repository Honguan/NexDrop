# Changelog

本檔案記錄 NexDrop 使用者可感知的變更，格式遵循 Keep a Changelog 與語意化版本。

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
