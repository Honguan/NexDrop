# NexDrop Windows 用戶端

[English](README.md)

Windows 10/11 x64 用戶端由 Flutter 桌面程式、背景整合程序與 LAN Bridge 組成。
傳送頁會在輸入時即時更新按鈕狀態，並支援 `Ctrl + Enter` 快速傳送。

## 開發建置

```powershell
cd client
flutter pub get
flutter analyze
flutter test
flutter build windows --release
```

請從儲存庫根目錄執行封裝。簽章是選填設定；有受保護的 PFX 時可一併提供：

```powershell
./client/windows/build-release.ps1 -Version (Get-Content VERSION)
./client/windows/build-release.ps1 -Version (Get-Content VERSION) -CertificatePath C:\signing\nexdrop.pfx -CertificatePassword $env:NEXDROP_CERTIFICATE_PASSWORD
```

產物位於 `dist/`，包含 Inno Setup 安裝程式與可攜 ZIP。未簽章產物仍可使用，但 Windows 可能顯示 SmartScreen 警告。簽章材料必須由受保護環境注入，不可提交至儲存庫。

目前只支援 Windows x64。本機防火牆必須允許設定的 LAN 傳輸連接埠，才能使用直接傳輸。
