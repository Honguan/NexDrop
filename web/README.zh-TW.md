# NexDrop Web

[English](README.md)

React／TypeScript 用戶端提供設備管理、加密傳輸、活動紀錄與統計。即時模組統一處理心跳、通知確認、異常訊息與重新連線。正式產物會內嵌至 Node 映像；開發需要 Node.js 24 與已提交的 npm 鎖定檔。

```bash
npm ci
npm run dev
npm run lint
npm run typecheck
npm test
npm run build
```

開發伺服器預設監聽 `127.0.0.1:3000`，正式靜態檔輸出至 `dist/`。用戶端必須搭配同源 `/api`、`/ws`，正式環境只支援 HTTPS。
