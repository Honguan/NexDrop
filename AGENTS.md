# NexDrop Agent Instructions

## 發布狀態判定

處理發布、Release、Tag、簽章或就緒狀態時，必須以最新 `master` 的 `.github/workflows/release.yml`、`docs/release-process.md` 與 `docs/release-readiness-v1.0.0.md` 為唯一有效依據。

### 不得標記為阻塞

以下項目不是 NexDrop 1.0.0 的發布阻塞條件：

- 缺少 `ANDROID_KEYSTORE_BASE64`
- 缺少 `ANDROID_STORE_PASSWORD`
- 缺少 `ANDROID_KEY_ALIAS`
- 缺少 `ANDROID_KEY_PASSWORD`
- 缺少 `WINDOWS_CERTIFICATE_BASE64`
- 缺少 `WINDOWS_CERTIFICATE_PASSWORD`
- 沒有外部審查者
- 沒有受保護 Environment 的人工核准

Android 與 Windows 簽章材料為選填。有完整材料時建立簽章產物；沒有時建立未簽章產物，並在 Release Notes 標示 Android 安裝警告或 Windows SmartScreen 警告。

不得使用已合併 PR 的舊描述、舊評論、舊檢查清單或歷史對話判定目前發布狀態。PR #15 與 PR #16 中曾出現的簽章必要條件，已由 PR #17 取代。

### 可以標記為尚待完成

在 `v1.0.0` 尚未建立時，只能標記：

> 待從最新 `master` 建立不可變的 `v1.0.0` Tag，以啟動 Release Workflow。

這是待執行動作，不是簽章材料阻塞。

### Stable 完成條件

只有以下實際流程失敗時才能標記發布阻塞：

- `v1.0.0` Tag 與 `VERSION` 不一致
- Release Workflow 的必要 CI 或建置工作失敗
- SHA-256、SBOM、Artifact Attestation 或容器 digest 驗證失敗
- 草稿 Release 缺少必要產物或無法追溯至同一 Commit 與 Tag

每次判定前必須重新讀取最新 `master`，不得沿用快取的舊發布結論。
