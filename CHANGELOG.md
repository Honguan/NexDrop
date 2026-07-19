# Changelog

本檔案記錄 NexDrop 使用者可感知的變更，格式遵循 Keep a Changelog 與語意化版本。

## [Unreleased]

## [2.0.0] - 2026-07-19

### Added

- 節點連結與節點密鑰的一鍵匯入／複製流程。
- 統一設備聊天室、圖片／檔案拖放、設備加入與新內容通知。
- 管理後台離線設備刪除、最後上線與在線狀態。
- 安裝完成輸出 IP 節點網址、節點密鑰、Web OTP 與可選來源 IP 白名單。

### Changed

- 一般設備直接使用節點密鑰加入，不再使用管理員帳密、OTP、配對碼、待核准或人工核准。
- 傳送、接收及傳輸紀錄整合為聊天室；預設廣播給全部設備。
- 指定設備內容改為設備層級可見，其他設備無法讀取該筆對話。

### Removed

- 第一方介面的配對碼、挑戰 ID、待核准設備與管理員配對操作。
- 節點設定中的六位數設備驗證碼。

## [1.0.6] - 2026-07-18

### Changed

- 帳號第一台有效設備作為信任起點；後續新增設備會進入待配對狀態，並由該設備自動產生挑戰 ID、六位數配對碼及 QR 配對資料。
- 已信任設備可在設備頁輸入配對資料核准同帳號的新設備，配對操作不再放在管理後台。

### Fixed

- 修正管理後台高頻同時載入多個 API，觸發速率限制後整頁沒有資料的問題；現在改為較低頻率刷新、局部容錯及手動重試。
- 修正 Windows EXE 登入前沒有系統匣圖示，以及最小化後圖示或視窗無法正確恢復的問題。

### Security

- 配對碼只能由待配對設備自己的有效工作階段產生，並只能由同帳號的已信任設備兌換。

## [1.0.5] - 2026-07-18

### Added

- 裝置列表與統計新增即時在線、最後上線、裝置類型及逐設備傳送／接收用量。
- Web 與桌面／Android 傳輸紀錄新增快速複製及確認刪除操作。

### Changed

- 同帳號向同一 Linux 節點登記裝置時自動信任；既有待配對與相容流程才需配對碼或人工核准。
- Web、桌面／Android 與瀏覽器擴充功能預設傳送給全部信任設備，使用者仍可逐台取消。
- 第一方用戶端移除群組入口，傳輸統計改為逐設備狀態與用量，節點狀態及資源每 5 秒更新。

## [1.0.4] - 2026-07-18

### Fixed

- 發布流程使用 `apksigner` 最終重簽並驗證 Android APK 的 v1／v2 簽章，且一般 Flutter CI 也會在建立 Tag 前執行相同檢查。

## [1.0.3] - 2026-07-18

### Fixed

- Android Release APK 明確啟用 v1 與 v2 簽章，修正 Samsung J6 與 Android 6 相容性，並符合發布簽章驗證。

## [1.0.2] - 2026-07-18

### Added

- 瀏覽器擴充功能可獨立配對為設備，在小視窗輸入內容、指定接收設備，並選擇是否附上目前網址。
- 新增 `credentials`、`configure-secrets` 與免填版本的安全更新流程，可逐項查看、輪替或重新隨機產生部署秘密。

### Changed

- 安裝器會自動取得 Docker 所需管理員權限、保留 `.env` 的原使用者擁有權，並顯示可修改的安全隨機預設。
- PostgreSQL 密碼與連線 URL 分離傳入，支援文件列出的特殊字元且保留舊版 URL 相容性。
- Android 未提供正式 keystore 時，發布流程改為建立具 v1/v2 臨時簽章的可安裝 APK，並驗證簽章後才交付。

### Fixed

- 修正 Samsung J6 因 APK 未簽章而顯示「應用程式套件無效」的問題。
- 修正擴充功能錯誤依賴桌面橋接，以及限流提示未顯示實際等待秒數的問題。

### Security

- 擴充功能改用按節點要求的選用網站權限，移除未使用的 Native Messaging 與通知權限，並在中斷配對時刪除本機裝置私鑰。

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
[1.0.2]: https://github.com/Honguan/NexDrop/releases/tag/v1.0.2
[1.0.3]: https://github.com/Honguan/NexDrop/releases/tag/v1.0.3
[1.0.4]: https://github.com/Honguan/NexDrop/releases/tag/v1.0.4
[1.0.5]: https://github.com/Honguan/NexDrop/releases/tag/v1.0.5
[1.0.6]: https://github.com/Honguan/NexDrop/releases/tag/v1.0.6

[2.0.0]: https://github.com/Honguan/NexDrop/releases/tag/v2.0.0
