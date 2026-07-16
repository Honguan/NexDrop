# NexDrop Extension

支援 Chrome 與 Edge 的 Manifest V3 擴充套件。

## 建置

```powershell
npm ci
npm run lint
npm run typecheck
npm test
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

正式 Chrome/Edge ZIP 使用 `npm run package` 輸出至根目錄 `dist/`。套件不包含 `.env` 或憑證；Native Messaging 需分別以實際瀏覽器擴充功能 ID 註冊。瀏覽器原則或使用者停用原生主機時無法使用桌面橋接。
