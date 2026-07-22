# NexDrop Web

[繁體中文](README.zh-TW.md)

The React and TypeScript client provides device management, encrypted transfer, activity, and statistics. Production assets are embedded in the Node image. Development requires Node.js 24 and the committed npm lockfile.

```bash
npm ci
npm run dev
npm run lint
npm run typecheck
npm test
npm run build
```

The development server listens on `127.0.0.1:3000` by default, and production assets are written to `dist/`. The client requires same-origin `/api` and `/ws` endpoints and supports HTTPS only in production.
