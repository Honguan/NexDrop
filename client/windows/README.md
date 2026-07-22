# NexDrop for Windows

[繁體中文](README.zh-TW.md)

The Windows 10/11 x64 client combines the Flutter desktop application, background integration process, and LAN bridge.
The send view updates its action state while typing and supports `Ctrl + Enter` for quick transfer.

## Development build

```powershell
cd client
flutter pub get
flutter analyze
flutter test
flutter build windows --release
```

Run packaging from the repository root. Signing is optional; provide a protected PFX when available:

```powershell
./client/windows/build-release.ps1 -Version (Get-Content VERSION)
./client/windows/build-release.ps1 -Version (Get-Content VERSION) -CertificatePath C:\signing\nexdrop.pfx -CertificatePassword $env:NEXDROP_CERTIFICATE_PASSWORD
```

Artifacts are written to `dist/` as an Inno Setup installer and a portable ZIP. Unsigned artifacts remain usable but can display a Windows SmartScreen warning. Signing material must be injected from a protected environment and must never be committed.

Windows x64 is currently the only supported Windows architecture. The local firewall must allow the configured LAN transfer port for direct transfers.
