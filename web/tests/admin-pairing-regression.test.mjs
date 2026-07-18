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

test("待核准設備自行產生配對碼並由信任設備核准", async () => {
  const app = await readFile(new URL("../src/App.tsx", import.meta.url), "utf8");
  assert.match(app, /第一台信任設備/);
  assert.match(app, /此設備配對碼/);
  assert.match(app, /pairing-code/);
  assert.match(app, /核准新設備/);
  assert.doesNotMatch(app, /請由管理員核准這個瀏覽器設備/);
  assert.doesNotMatch(app, /設備已核准/);
});
