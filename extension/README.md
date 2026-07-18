# NexDrop Extension

支援 Chrome 與 Edge 的 Manifest V3 擴充套件。擴充功能與 Windows 桌面程式是不同設備，兩者必須各自向 NexDrop 節點配對。

## 配對與使用

1. 開啟擴充功能的「配對設定」，填入 HTTPS 節點網址、設備名稱、帳號與密碼；啟用 TOTP 時也要填六位數驗證碼。
2. 同意只針對該節點的網站存取權限。擴充功能會在本機建立 X25519 金鑰並登記為獨立設備。
3. 到 NexDrop Web 的「設備」頁核准這台擴充功能，再重新開啟小視窗。
4. 在小視窗輸入文字，選擇一台特定設備，並決定是否附上目前分頁網址後傳送。

登入權杖與裝置私鑰只保存在擴充功能的本機儲存區。中斷配對後，仍應到 NexDrop Web 撤銷不再使用的設備紀錄。

## 建置

```powershell
npm ci
npm run lint
npm run typecheck
npm test
npm run build
```

在瀏覽器擴充功能頁開啟開發人員模式，載入 `dist` 目錄。正式 Chrome／Edge ZIP 使用 `npm run package` 輸出至根目錄 `dist/`，套件不包含 `.env`、Token 或憑證。

新版擴充功能不要求 Native Messaging 或通知權限，也不需要桌面程式保持連線。
