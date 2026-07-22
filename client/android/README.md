# NexDrop for Android

[繁體中文](README.zh-TW.md)

The Flutter Android client uses application ID `io.github.honguan.nexdrop`.

## Development build

```powershell
cd client
flutter pub get
flutter analyze
flutter test
flutter build apk --debug
```

For a stable release identity, use protected GitHub Environment secrets to generate `android/key.properties` and a persistent keystore, then run:

```powershell
flutter build apk --release
```

Release builds never fall back to the debug key. When formal secrets are absent, the official workflow creates a temporary 30-day keystore and produces an installable APK with v1 and v2 signatures. This fixes the "invalid application package" failure on devices such as the Samsung J6, but a later temporary key can require uninstalling the previous build. Only a persistent keystore supports in-place updates; losing it prevents future updates under the same application ID.

The configured minimum Android version includes Android 8 devices such as the Samsung J6. The artifact is `build/app/outputs/flutter-apk/app-release.apk`. Before publishing, verify signatures and certificates with `apksigner verify --verbose --print-certs`. Build machines must accept all Android SDK and NDK licenses required by Flutter.
