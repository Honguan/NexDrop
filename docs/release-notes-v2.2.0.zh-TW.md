# NexDrop 2.2.0

[English](release-notes-v2.2.0.md)

NexDrop 2.2.0 新增隱私安全的傳輸可觀測性與可重現的復原診斷。

## 重點

- 每筆已授權傳輸都可查看不含內容的有序時間軸，並以穩定事件碼關聯檔案、目標、路由、execution 與錯誤。
- 可執行 `./deploy/nexdrop diagnostics --output diagnostics.zip` 建立唯讀且自動遮蔽秘密的支援診斷包。
- HTTP、WebSocket、儲存與清理活動會以 request 與 transfer 識別碼關聯，且不記錄訊息或檔案內容。
- 提供有限基數操作指標標籤、初始 SLO、穩定錯誤碼處置，以及隔離式故障注入情境。
- 負載驗證報告新增產品版本、建置 Commit、環境、成功率及延遲百分位數。

## 更新

```bash
./deploy/nexdrop update 2.2.0
```

更新會保留 `.env`、PostgreSQL 資料、檔案資料與既有秘密，並在切換映像前建立備份。

若 Release 使用臨時 Android 簽章，已安裝舊版 APK 的裝置可能需要先移除舊版再安裝；正式環境應設定固定 Android 簽章 Secrets。未提供 Windows 憑證時，EXE 與 ZIP 仍可使用，但 Windows 可能顯示 SmartScreen 警告。
