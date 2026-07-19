import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("Chrome 與 Edge 使用獨立 Manifest V3，節點權限在加入時要求", async () => {
  const packageMetadata = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  for (const path of ["../manifests/chrome.json", "../manifests/edge.json"]) {
    const manifest = JSON.parse(await readFile(new URL(path, import.meta.url), "utf8"));
    assert.equal(manifest.manifest_version, 3);
    assert.equal(manifest.version, packageMetadata.version);
    assert.deepEqual(manifest.host_permissions ?? [], []);
    assert.deepEqual(manifest.optional_host_permissions, ["https://*/*", "http://*/*"]);
    assert.ok(manifest.permissions.includes("notifications"));
  }
});

test("擴充功能使用節點密鑰、預設全選設備並顯示通知", async () => {
  const popup = await readFile(new URL("../popup.html", import.meta.url), "utf8");
  const popupCode = await readFile(new URL("../src/popup.ts", import.meta.url), "utf8");
  const directCode = await readFile(new URL("../src/direct.ts", import.meta.url), "utf8");
  const workerCode = await readFile(new URL("../src/service-worker.ts", import.meta.url), "utf8");
  const options = await readFile(new URL("../options.html", import.meta.url), "utf8");
  assert.match(popup, /<textarea id="content"/);
  assert.match(popupCode, /input\.checked = true/);
  assert.match(directCode, /\/api\/node\/enroll/);
  assert.doesNotMatch(directCode, /\/api\/auth\/login|pending:/);
  assert.match(workerCode, /chrome\.notifications\.create/);
  assert.match(options, /節點密鑰/);
  assert.match(options, /從剪貼簿匯入/);
  assert.doesNotMatch(options, /帳號或電子郵件|六位數驗證碼|配對碼/);
});
