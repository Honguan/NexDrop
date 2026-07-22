# NexDrop 瀏覽器擴充功能

[English](README.md)

Manifest V3 擴充功能支援 Chrome 與 Edge。它是獨立的 NexDrop 設備，不是 Windows 桌面程式的別名，因此會保存自己的設備身分與登入工作階段。

## 配對與使用

1. 開啟「配對設定」，填入 HTTPS Node 網址、設備名稱、帳號與密碼；啟用 TOTP 時也要填六位數驗證碼。
2. 只授權該 Node 的網站存取權限。擴充功能會在本機建立 X25519 金鑰並登記為獨立設備。
3. 同一帳號登入同一 Linux Node 時會自動信任；只有既有待配對設備或跨節點情境才需要在「設備」頁核准。
4. 在小視窗輸入文字、選擇是否附上目前分頁網址，再選擇接收設備；信任設備預設全選。未送出的文字會保存在本機，也可按 `Ctrl／Command + Enter` 快速傳送。

登入權杖與設備私鑰只保存在擴充功能的本機儲存區。中斷本機配對不會刪除伺服器紀錄；不再使用的設備仍應從 NexDrop Web 撤銷。

## 建置

```powershell
npm ci
npm run lint
npm run typecheck
npm test
npm run build
```

在瀏覽器擴充功能頁開啟開發人員模式並載入 `dist/`。執行 `npm run package` 會在儲存庫 `dist/` 產生分開的 Chrome／Edge 壓縮檔，且不包含 `.env`、Token 或憑證。

目前版本不要求 Native Messaging 或通知權限，也不依賴桌面程式保持執行。
