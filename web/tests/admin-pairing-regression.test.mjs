import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("管理後台可局部載入且輪詢不會超過預設限制", async () => {
  const app = await readFile(new URL("../src/App.tsx", import.meta.url), "utf8");
  assert.match(app, /Promise\.allSettled/);
  assert.match(app, /30_000/);
  assert.match(app, /部分管理資料暫時無法載入/);
  assert.match(app, />重試</);
});

test("2.0 移除配對介面並統一為聊天室", async () => {
  const app = await readFile(new URL("../src/App.tsx", import.meta.url), "utf8");
  assert.match(app, /節點聊天室/);
  assert.match(app, /chat-shell/);
  assert.match(app, /onDrop/);
  assert.match(app, /離線設備已刪除/);
  assert.doesNotMatch(app, /pairing-code|配對碼|核准新設備|此設備配對碼/);
});
