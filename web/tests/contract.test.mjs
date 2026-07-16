import assert from "node:assert/strict";
import test from "node:test";

test("新版與舊版錯誤都保留穩定代碼", () => {
  const code = (body) => typeof body.error === "string" ? body.error : body.error?.code;
  assert.equal(code({ error: "INVALID_TOKEN" }), "INVALID_TOKEN");
  assert.equal(code({ error: { code: "INVALID_TOKEN" } }), "INVALID_TOKEN");
});
