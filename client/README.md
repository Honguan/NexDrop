# NexDrop Client

共用 Flutter 客戶端，正式支援 Windows 10/11 與 Android。

## 功能

- HTTPS 節點登入、Token 更新與 WebSocket 在線狀態
- X25519、HKDF-SHA-256、AES-256-GCM 端對端加密
- 文字、網址及多檔分段節點傳輸
- 每設備獨立包裝內容金鑰
- Windows 系統匣與關閉時隱藏
- Android/Windows 平台安全儲存
- SQLite 本機歷史、草稿、等待區網任務與統計佇列

## 驗證

```powershell
flutter analyze
flutter test
flutter build windows --release
flutter build apk --release
```

Android 建置需要接受 Android SDK 授權並安裝對應 SDK 平台。
