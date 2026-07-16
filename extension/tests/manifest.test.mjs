import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("擴充功能使用 Manifest V3 且不要求廣域主機權限", async () => {
  const manifest = JSON.parse(await readFile(new URL("../manifest.json", import.meta.url), "utf8"));
  assert.equal(manifest.manifest_version, 3);
  assert.equal(manifest.version, "1.0.0");
  assert.deepEqual(manifest.host_permissions ?? [], []);
});
