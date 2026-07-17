import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("Chrome 與 Edge 使用獨立的 Manifest V3 且不要求廣域主機權限", async () => {
  for (const path of ["../manifests/chrome.json", "../manifests/edge.json"]) {
    const manifest = JSON.parse(await readFile(new URL(path, import.meta.url), "utf8"));
    assert.equal(manifest.manifest_version, 3);
    assert.equal(manifest.version, "1.0.0");
    assert.deepEqual(manifest.host_permissions ?? [], []);
  }
});
