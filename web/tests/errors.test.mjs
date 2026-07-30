import assert from "node:assert/strict";
import test from "node:test";

import { messageFor, rateLimitMessage } from "../src/errors.ts";

function apiError(code, status, retryAfterSeconds, details) {
  return Object.assign(new Error(code), { code, status, retryAfterSeconds, details });
}

test("限流錯誤顯示等待秒數", () => {
  assert.equal(
    rateLimitMessage({ code: "RATE_LIMITED", retryAfterSeconds: 42 }),
    "操作過於頻繁，請在 42 秒後再試。",
  );
  assert.equal(
    rateLimitMessage({ code: "RATE_LIMITED" }),
    "操作過於頻繁，請稍後再試。",
  );
  assert.equal(rateLimitMessage({ code: "INVALID_REQUEST" }), null);
});

test("API errors use stable, actionable messages", () => {
  assert.equal(messageFor(apiError("INVALID_CREDENTIALS", 401)), "帳號或密碼不正確");
  assert.equal(messageFor(apiError("RATE_LIMITED", 429, 8)), "操作過於頻繁，請在 8 秒後再試。");
  assert.equal(messageFor(apiError("NEW_SERVER_CODE", 400)), "操作失敗：NEW_SERVER_CODE");
  assert.equal(
    messageFor(apiError("CAPABILITY_UNAVAILABLE", 409, undefined, {
      capability: "resumable_chunks",
      party: "node",
    })),
    "目前的節點缺少 resumable_chunks 相容能力，請更新後再試。",
  );
  assert.equal(messageFor(new Error("network unavailable")), "network unavailable");
  assert.equal(messageFor(null), "操作失敗，請稍後再試");
});
