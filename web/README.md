# NexDrop Web

React/TypeScript 管理與傳輸介面，由 Node 映像內嵌提供。需求為 Node.js 24 與 npm 鎖定檔。

```bash
npm ci
npm run dev
npm run lint
npm run typecheck
npm test
npm run build
```

開發伺服器預設 `127.0.0.1:3000`；正式靜態檔輸出 `dist/`。Web 必須與同源 `/api`、`/ws` 搭配，正式環境只支援 HTTPS。
