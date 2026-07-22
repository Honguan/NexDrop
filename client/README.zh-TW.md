# NexDrop 用戶端

[English](README.md)

平台專屬建置方式與限制請參閱 [Windows](windows/README.zh-TW.md) 與 [Android](android/README.zh-TW.md)。

共用 Flutter 用戶端正式支援 Windows 10/11 與 Android。

## 功能

- HTTPS Node 登入、Token 更新與 WebSocket 在線狀態
- X25519、HKDF-SHA-256、AES-256-GCM 端對端加密
- 文字、網址及多檔分段 Node 傳輸
- 每台接收設備獨立包裝內容金鑰
- 即時傳送按鈕狀態與桌面 `Ctrl/Command + Enter` 快速傳送
- Windows 系統匣、最小化隱藏與明確退出背景程序
- Android／Windows 使用 `nexdrop://join` 帶入 Node 網址與密鑰
- Android／Windows 平台安全儲存
- SQLite 本機歷史、草稿、等待區網任務與統計佇列

傳送工作區與共用的可重新整理頁面外框各自封裝為 UI 模組，使選檔、快捷鍵與重新整理行為可獨立測試，不與應用程式啟動流程耦合。

## 需求與開發

需要 Flutter stable，以及 Windows 10/11 開發工具或 Android SDK。應用程式設定必須存放於平台安全儲存，不可寫入原始碼。

```powershell
flutter pub get
flutter analyze
flutter test
flutter run -d windows
flutter run -d android
```

## 正式建置

```powershell
flutter analyze
flutter test
flutter build windows --release
flutter build apk --release
```

Windows 完整封裝使用 `windows/build-release.ps1`，輸出至儲存庫 `dist/`。Android 建置需要接受 SDK 授權，Release 不會使用 debug key；建議使用固定且未提交的 keystore，讓後續版本能直接覆蓋安裝。用戶端需要可連線的 HTTPS Node，區網發現亦可能受防火牆與網路隔離影響。
