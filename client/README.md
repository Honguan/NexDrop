# NexDrop Client

平台專屬建置與限制請參閱 [Windows](windows/README.md) 與 [Android](android/README.md)。

共用 Flutter 客戶端，正式支援 Windows 10/11 與 Android。

## 功能

- HTTPS 節點登入、Token 更新與 WebSocket 在線狀態
- X25519、HKDF-SHA-256、AES-256-GCM 端對端加密
- 文字、網址及多檔分段節點傳輸
- 每設備獨立包裝內容金鑰
- Windows 系統匣與關閉時隱藏
- Android/Windows 平台安全儲存
- SQLite 本機歷史、草稿、等待區網任務與統計佇列

## 需求與開發

需要 Flutter stable、Windows 10/11 開發工具或 Android SDK。設定檔由應用程式安全儲存管理，不應寫入原始碼。

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

Windows 完整封裝使用 `windows/build-release.ps1`，輸出至根目錄 `dist/`。Android 建置需要接受 SDK 授權；Release 必須在 `android/key.properties` 指向未提交的正式 keystore，未設定時不會使用 debug key。Client 依賴可連線的 HTTPS Node，區網發現亦受作業系統防火牆與網路隔離限制。
