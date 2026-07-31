# NexDrop 3.0.0

[English](release-notes-v3.0.0.md)

自適應路由、復原、安全註冊、Relay Pool、資料夾與訊息生命週期

## 更新

```bash
./deploy/nexdrop update 3.0.0
```

更新會保留 `.env`、PostgreSQL 資料、檔案資料與既有秘密，並在切換映像前建立備份。

若 Release 使用臨時 Android 簽章，已安裝舊版 APK 的裝置可能需要先移除舊版再安裝；正式環境應設定固定 Android 簽章 Secrets。未提供 Windows 憑證時，EXE 與 ZIP 仍可使用，但 Windows 可能顯示 SmartScreen 警告。

## 驗證

功能版本在調整版本號前，已通過 Server、PostgreSQL Integration、Flutter、Web、瀏覽器擴充套件、Docker、文件與 Security 工作流程。
