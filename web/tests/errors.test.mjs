import assert from "node:assert/strict";
import test from "node:test";

import { rateLimitMessage } from "../src/errors.ts";

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
