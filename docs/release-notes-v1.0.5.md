# NexDrop 1.0.5

此版本重新整理多裝置傳輸體驗：同一帳號向同一 Linux 節點登記 Windows、Android、Web 或瀏覽器擴充功能時會自動信任，不再要求使用者尋找不存在的配對碼。既有待配對資料與舊版 API 仍可使用原有核准流程。

Web、Windows、Android、Chrome 與 Edge 現在預設選取所有信任設備，仍可在傳送前逐台取消。第一方介面已移除群組入口；舊版群組 API 暫時保留，以維持 1.x 相容性。

設備狀態、傳輸進度與節點狀態會持續更新。統計頁改為逐台顯示在線狀態、最後上線時間、傳送／接收筆數與流量，不再堆疊節點 CPU 歷史取樣。文字與網址傳輸紀錄新增快速複製及確認刪除操作。

VPS 更新可執行 `./deploy/nexdrop update` 自動取得最新正式版本，或執行 `./deploy/nexdrop update 1.0.5` 鎖定此版本；更新會保留 `.env`、資料卷與既有秘密。

若 Android APK 使用發布流程產生的臨時簽章，而裝置已安裝不同簽章的舊版，Android 可能要求先解除安裝舊版。要直接覆蓋更新，請設定固定的 Android 正式簽章 Secrets。未提供 Windows 憑證時，安裝程式可能顯示 SmartScreen 警告。
