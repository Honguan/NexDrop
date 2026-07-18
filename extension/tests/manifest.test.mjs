import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("Chrome 與 Edge 使用獨立的 Manifest V3，節點權限只在配對時要求", async () => {
  for (const path of ["../manifests/chrome.json", "../manifests/edge.json"]) {
    const manifest = JSON.parse(await readFile(new URL(path, import.meta.url), "utf8"));
    assert.equal(manifest.manifest_version, 3);
    assert.equal(manifest.version, "1.0.1");
    assert.deepEqual(manifest.host_permissions ?? [], []);
    assert.deepEqual(manifest.optional_host_permissions, ["https://*/*", "http://localhost/*", "http://127.0.0.1/*"]);
  }
});

test("小視窗提供內容、網址選項與特定設備傳送，且不依賴桌面連線", async () => {
  const popup = await readFile(new URL("../popup.html", import.meta.url), "utf8");
  const popupCode = await readFile(new URL("../src/popup.ts", import.meta.url), "utf8");
  const directCode = await readFile(new URL("../src/direct.ts", import.meta.url), "utf8");
  const options = await readFile(new URL("../options.html", import.meta.url), "utf8");
  assert.match(popup, /<textarea id="content"/);
  assert.match(popup, /id="include-url"/);
  assert.match(popupCode, /input\.type = "radio"/);
  assert.doesNotMatch(popupCode, /requestNative|Desktop/);
  assert.match(directCode, /Retry-After/);
  assert.match(options, /將擴充功能配對為獨立設備/);
});
