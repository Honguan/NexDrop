import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("第一方 Web 預設全選設備、提供複製刪除並移除群組入口", async () => {
  const app = await readFile(new URL("../src/App.tsx", import.meta.url), "utf8");
  assert.match(app, /trusted\.map\(\(item\) => item\.id\)/);
  assert.match(app, /navigator\.clipboard\.writeText/);
  assert.match(app, />刪除</);
  assert.doesNotMatch(app, /id: "groups"/);
});

test("Web 會定期刷新設備、傳輸與即時節點狀態", async () => {
  const app = await readFile(new URL("../src/App.tsx", import.meta.url), "utf8");
  assert.match(app, /window\.setInterval\(.*reload/s);
  assert.match(app, /item\.online/);
  assert.match(app, /最後更新/);
  assert.match(app, /settingsInitialized\.current/);
});
