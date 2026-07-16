# NexDrop Windows 用戶端

Windows 10/11 x64 用戶端由 Flutter 桌面程式、背景服務與 LAN Bridge 組成。

## 開發建置

```powershell
cd client
flutter pub get
flutter analyze
flutter test
flutter build windows --release
```

正式封裝請在儲存庫根目錄執行：

```powershell
./client/windows/build-release.ps1 -Version (Get-Content VERSION) -CertificatePath C:\signing\nexdrop.pfx -CertificatePassword $env:NEXDROP_CERTIFICATE_PASSWORD
```

產物位於 `dist/`，包含 Inno Setup 安裝程式與可攜 ZIP。Release 必須使用受保護環境注入的憑證簽章；腳本不接受未簽章正式產物。

目前僅支援 Windows x64，背景服務需要本機防火牆允許已設定的 LAN 傳輸連接埠。
