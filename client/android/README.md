# NexDrop Android 用戶端

Android 用戶端使用 Flutter，application ID 為 `io.github.honguan.nexdrop`。

## 開發建置

```powershell
cd client
flutter pub get
flutter analyze
flutter test
flutter build apk --debug
```

正式建置建議由受保護的 GitHub Environment Secrets 產生 `android/key.properties` 與固定 keystore，再執行：

```powershell
flutter build apk --release
```

Release 不會回退使用 debug key。官方 Workflow 未設定正式 Secrets 時會建立有效期限 30 天的臨時 keystore，產生同時具 v1/v2 簽章的可安裝 APK；這能修正 Samsung J6 的「應用程式套件無效」，但下一版可能需要先移除舊版。固定 keystore 才能讓 Android 直接覆蓋更新，金鑰遺失後無法用同一 application ID 更新既有安裝。

最低版本沿用 Flutter 專案設定，包含 Samsung J6 的 Android 8。產物為 `build/app/outputs/flutter-apk/app-release.apk`；發布前必須使用 `apksigner verify --verbose --print-certs` 驗證 v1/v2 簽章與憑證。Android 建置機需預先接受 Flutter 所需 Android SDK 與 NDK 授權。
