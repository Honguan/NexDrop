import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("第一方 Web 預設全選設備、提供複製刪除並移除群組入口", async () => {
  const app = await readFile(new URL("../src/App.tsx", import.meta.url), "utf8");
  assert.match(app, /trusted\.map\(\(item\) => item\.id\)/);
  assert.match(app, /navigator\.clipboard\.writeText/);
  assert.match(app, />\s*刪除\s*</);
  assert.match(app, /已複製節點導入資料/);
  assert.match(app, /無法存取剪貼簿，請手動複製/);
  assert.doesNotMatch(app, /id: "groups"/);
});

test("Web 會定期刷新設備與傳輸並維持即時狀態", async () => {
  const app = await readFile(new URL("../src/App.tsx", import.meta.url), "utf8");
  const realtime = await readFile(new URL("../src/realtime.ts", import.meta.url), "utf8");
  assert.match(app, /window\.setInterval\(.*reload/s);
  assert.match(app, /item\.online/);
  assert.match(app, /subscribeNodeEvents/);
  assert.match(realtime, /socket\.onmessage/);
  assert.match(realtime, /type: "heartbeat"/);
  assert.doesNotMatch(app, /\/api\/admin\//);
});
