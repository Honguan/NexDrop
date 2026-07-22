import assert from "node:assert/strict";
import test from "node:test";

import { fileMetadata, formatBytes, formatDate, labelDeviceType, statusLabel, successRate } from "../src/presentation.ts";

test("presentation helpers handle empty and malformed values", () => {
  assert.equal(formatBytes(0), "0 B");
  assert.equal(formatBytes(-1), "0 B");
  assert.equal(formatBytes(Number.NaN), "0 B");
  assert.equal(formatBytes(1536), "1.5 KB");
  assert.equal(formatDate("not-a-date"), "—");
  assert.equal(fileMetadata("not-json", 0), undefined);
  assert.equal(fileMetadata('{"name":"not-an-array"}', 0), undefined);
});

test("presentation helpers preserve known labels and metadata", () => {
  assert.equal(labelDeviceType("WEB_EDGE"), "Edge Web");
  assert.equal(labelDeviceType("FUTURE_DEVICE"), "FUTURE_DEVICE");
  assert.equal(statusLabel("WAITING_FOR_NODE"), "WAITING FOR NODE");
  assert.deepEqual(fileMetadata('[{"name":"report.pdf","mimeType":"application/pdf","size":42}]', 0), {
    name: "report.pdf",
    mimeType: "application/pdf",
    size: 42,
  });
  assert.equal(successRate({ succeeded: 3, failed: 1 }), 75);
  assert.equal(successRate({ succeeded: 0, failed: 0 }), 0);
});
