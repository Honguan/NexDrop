# NexDrop 1.0.4

此修正版會在 Android APK 建置後使用 Android 官方 `apksigner` 最終套用並驗證 v1（JAR）與 v2 簽章，修正 Flutter／AGP 最終產物僅保留 v2 簽章的問題。相同檢查已加入一般 Flutter CI，往後會在合併與建立 Tag 前攔截簽章不完整的 APK。

Samsung J6 的 Android 8 支援 v2 簽章；額外保留 v1 簽章可維持舊版 Android 相容性。其餘 1.0.2 功能維持不變，包含互動式安裝與更新、密碼與秘密設定、獨立裝置配對、瀏覽器擴充功能傳送文字／網址，以及 Windows、Linux Node、Chrome、Edge 與 Android 產物。

VPS 更新可執行 `./deploy/nexdrop update` 自動取得最新正式版本，或執行 `./deploy/nexdrop update 1.0.4` 鎖定此版本；更新會保留 `.env`、資料卷與既有秘密。

若 APK 使用發布流程產生的臨時簽章，而裝置已安裝不同簽章的舊版，Android 會要求先解除安裝舊版。要直接覆蓋更新，請設定固定的 Android 正式簽章 Secrets。
