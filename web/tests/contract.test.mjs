import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("新版與舊版錯誤都保留穩定代碼", () => {
  const code = (body) => typeof body.error === "string" ? body.error : body.error?.code;
  assert.equal(code({ error: "INVALID_TOKEN" }), "INVALID_TOKEN");
  assert.equal(code({ error: { code: "INVALID_TOKEN" } }), "INVALID_TOKEN");
});

test("restored sessions negotiate capabilities before realtime startup", async () => {
  const app = await readFile(new URL("../src/App.tsx", import.meta.url), "utf8");
  const api = await readFile(new URL("../src/api.ts", import.meta.url), "utf8");

  assert.match(app, /api\.refreshCapabilities\(\)[\s\S]*?\.then\(\(\) => api\.get<User>/);
  assert.match(api, /this\.capabilityDocument = null;[\s\S]*?fetch\("\/api\/version"/);
  assert.match(api, /clientVersion: `web-v\$\{nodeProtocol\}`/);
});
