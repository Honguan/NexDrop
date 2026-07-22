# NexDrop 2.0.2

NexDrop 2.0.2 發布設備加入與用戶端穩定性修正，並同步 2.x Release Workflow 標籤。

## 重點

- 修正 Windows 關閉視窗或選擇完全退出後，系統匣、連線與背景服務仍持續執行的問題。
- 支援 Windows 與 Android 使用 `nexdrop://join` 一鍵帶入節點連結及節點密鑰。
- 移除 Web 管理後台、瀏覽器管理 API、配對碼與手動核准流程，節點維運統一由部署命令處理。
- 修正設備加入、頁面排版、錯誤提示關閉、生命週期清理及負載測試仍依賴舊核准 API 的問題。
- Release Workflow 會依版本自動推送 2.x 浮動容器標籤，為可信自動產生 PR 啟用檢查通過後自動合併，並要求功能性 PR 同步更新發布版本與 Release Notes。

## 升級

```bash
./deploy/nexdrop update 2.0.2
```

升級前請先備份資料。若使用臨時簽章 Android APK，覆蓋安裝可能需要先移除舊版 APK；設定固定 Android 正式簽章 Secrets 後可維持後續覆蓋更新。
