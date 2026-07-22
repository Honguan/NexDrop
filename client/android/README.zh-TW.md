# NexDrop Android 用戶端

[English](README.md)

Flutter Android 用戶端的 application ID 為 `io.github.honguan.nexdrop`。

## 開發建置

```powershell
cd client
flutter pub get
flutter analyze
flutter test
flutter build apk --debug
```

需要固定的正式身分時，請由受保護的 GitHub Environment Secrets 產生 `android/key.properties` 與長期 keystore，再執行：

```powershell
flutter build apk --release
```

Release 不會回退使用 debug key。未設定正式 Secrets 時，官方工作流會建立有效期限 30 天的臨時 keystore，產生具 v1／v2 簽章的可安裝 APK。這能修正 Samsung J6 等設備的「應用程式套件無效」，但後續版本使用不同臨時金鑰時可能需要先移除舊版。只有固定 keystore 能直接覆蓋更新；金鑰遺失後無法用相同 application ID 更新既有安裝。

最低 Android 版本包含 Samsung J6 等 Android 8 設備。產物為 `build/app/outputs/flutter-apk/app-release.apk`；發布前必須以 `apksigner verify --verbose --print-certs` 驗證簽章與憑證。建置機需接受 Flutter 所需的 Android SDK 與 NDK 授權。
