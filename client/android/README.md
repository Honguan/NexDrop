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

正式建置需由受保護的 GitHub Environment Secrets 產生 `android/key.properties` 與 keystore，再執行：

```powershell
flutter build apk --release
```

Release 不會回退使用 debug key。產物為 `build/app/outputs/flutter-apk/app-release.apk`；發布前須驗證 APK 簽章。Android 建置機需預先接受 Flutter 所需 Android SDK 與 NDK 授權。
