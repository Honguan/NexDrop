# NexDrop 2.0.4

[English](release-notes-v2.0.4.md)

改善雙語文件、API 與即時連線邊界、擴充功能操作效率、Flutter 傳送與連線可靠性、部署診斷，以及 Go 1.26 安全自動化。

## 重點更新

- 瀏覽器擴充功能會保存小視窗草稿、顯示字數、支援 `Ctrl/Command + Enter`，並在執行錯誤後正確恢復傳送按鈕。
- Web 與 Flutter 的即時連線透過專用介面負責心跳、通知確認、損壞訊息隔離、重連及關閉生命週期。
- Go API 的請求解析、回應輸出、版本化錯誤與速率限制具有更清楚的模組邊界；限流重試時間會一致使用注入的時鐘。
- 目前的專案、元件、架構、維運、安全與發布文件均以英文為主，並提供完整繁中對照版本。
- 安裝診斷可在需要時顯示已保存的 Bootstrap 登入資料，日常狀態則不會暴露秘密；更新流程會保留秘密與 PostgreSQL 狀態。
- 安全 CI 改用相容 Go 1.26 的漏洞分析器與明確的寬鬆授權允許清單。

## 更新

```bash
./deploy/nexdrop update 2.0.4
```

更新會保留 `.env`、PostgreSQL 資料、檔案資料與既有秘密，並在切換映像前建立備份。

若 Release 使用臨時 Android 簽章，已安裝舊版 APK 的裝置可能需要先移除舊版再安裝；正式環境應設定固定 Android 簽章 Secrets。未提供 Windows 憑證時，EXE 與 ZIP 仍可使用，但 Windows 可能顯示 SmartScreen 警告。
