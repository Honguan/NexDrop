# NexDrop client

[繁體中文](README.zh-TW.md)

Platform-specific build instructions and limitations are documented for [Windows](windows/README.md) and [Android](android/README.md).

The shared Flutter client officially supports Windows 10/11 and Android.

## Capabilities

- HTTPS Node login, token refresh, and WebSocket presence
- X25519, HKDF-SHA-256, and AES-256-GCM end-to-end encryption
- Text, URL, and resumable multi-file Node transfer
- Independently wrapped content keys for every receiving device
- Live send-button state and `Ctrl/Command + Enter` desktop sending
- Windows tray, hide-on-minimize, and explicit background-process exit
- `nexdrop://join` onboarding for Node URLs and keys on Android and Windows
- Platform-secure storage on Android and Windows
- Local SQLite history, drafts, pending LAN tasks, and statistics queue

The send workspace and the shared refreshable page shell are isolated UI modules. This keeps file selection, keyboard shortcuts, and refresh behavior independently testable without coupling them to application startup.

## Requirements and development

Use Flutter stable with either the Windows 10/11 development tools or an Android SDK. Application settings belong in platform-secure storage and must not be embedded in source code.

```powershell
flutter pub get
flutter analyze
flutter test
flutter run -d windows
flutter run -d android
```

## Release builds

```powershell
flutter analyze
flutter test
flutter build windows --release
flutter build apk --release
```

Package Windows with `windows/build-release.ps1`; artifacts are written to the repository `dist/` directory. Android builds require accepted SDK licenses. Release builds never use the debug key. A stable, uncommitted keystore is recommended so later Android versions can update an installed application in place. The client requires a reachable HTTPS Node; LAN discovery can also be affected by firewalls and network isolation.
