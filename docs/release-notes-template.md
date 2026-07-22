# NexDrop {{VERSION}}

{{SUMMARY}}

## 更新

```bash
./deploy/nexdrop update {{VERSION}}
```

更新會保留 `.env`、PostgreSQL 資料、檔案資料與既有秘密，並在切換映像前建立備份。

若 Release 使用臨時 Android 簽章，已安裝舊版 APK 的裝置可能需要先移除舊版再安裝；正式環境應設定固定 Android 簽章 Secrets。未提供 Windows 憑證時，EXE 與 ZIP 仍可使用，但 Windows 可能顯示 SmartScreen 警告。
