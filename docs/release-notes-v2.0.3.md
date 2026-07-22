# NexDrop 2.0.3

NexDrop 2.0.3 是維護版本，整合 GitHub Actions 相依更新與自動合併修正。

## 變更摘要

- CodeQL 工作流程統一升級至 4.37.3，並將後續 CodeQL Dependabot 更新分組。
- Docker Buildx Action 升級至 4.2.0。
- Checkout Action 升級至 7.0.1，排除舊版 Node.js 執行環境警告。
- 修正可信任機器人作者的比對邏輯，避免自動合併工作流程誤判。
- 已解決 Actions 更新在容器與發布工作流程中的合併衝突。

## 更新

```bash
./deploy/nexdrop update 2.0.3
```

更新會保留 `.env`、PostgreSQL 資料、檔案資料與既有秘密，並在切換映像前建立備份。

若 Release 使用臨時 Android 簽章，已安裝舊版 APK 的裝置可能需要先移除舊版再安裝；正式環境應設定固定 Android 簽章 Secrets。未提供 Windows 憑證時，EXE 與 ZIP 仍可使用，但 Windows 可能顯示 SmartScreen 警告。
