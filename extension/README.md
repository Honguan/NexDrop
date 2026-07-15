# NexDrop Extension

支援 Chrome 與 Edge 的 Manifest V3 擴充套件。

## 建置

```powershell
npm install
npm run build
```

在瀏覽器擴充功能頁開啟開發人員模式，載入 `dist` 目錄。

## Native Messaging

先建置原生主機：

```powershell
go build -o nexdrop-bridge.exe ./cmd/nexdrop-bridge
```

再以擴充套件 ID 安裝 Chrome 與 Edge 主機資訊：

```powershell
./extension/native/install.ps1 -ExtensionId <擴充套件ID> -HostPath ./nexdrop-bridge.exe
```

Desktop 會在 `%LOCALAPPDATA%\NexDrop\bridge.json` 寫入僅供本機橋接使用的 URL 與權杖。
