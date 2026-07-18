# NexDrop 1.0.3

此修正版讓 Android Release APK 同時使用 v1（JAR）與 v2 簽章，修正 Samsung J6 與 Android 6 裝置可能無法安裝的問題。發布流程會驗證兩種簽章，避免再次產生不相容的 APK。

其餘 1.0.2 功能維持不變，包含互動式安裝與更新、密碼與秘密設定、獨立裝置配對、瀏覽器擴充功能傳送文字／網址，以及 Windows、Linux Node、Chrome、Edge 與 Android 產物。

VPS 更新可執行 `./deploy/nexdrop update` 自動取得最新正式版本，或執行 `./deploy/nexdrop update 1.0.3` 鎖定此版本；更新會保留 `.env`、資料卷與既有秘密。

若 APK 使用發布流程產生的臨時簽章，而裝置已安裝不同簽章的舊版，Android 會要求先解除安裝舊版。正式 Android keystore 可透過受保護的 GitHub Secrets 注入。
