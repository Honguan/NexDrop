# NexDrop 1.0.0 發布就緒證據

本文件記錄 `1.0.0` 的實際驗證狀態。只有所有必要項目皆為「通過」，且正式產物可由 Commit、Tag、雜湊與簽章追溯時，才可發布 Stable Release。

## 已通過

| 項目 | 證據 |
| --- | --- |
| 交付變更合併 | [PR #15](https://github.com/Honguan/NexDrop/pull/15) 的必要檢查全部成功，並合併為 Commit `47c56fc3262cbe658d191fe6a7f6a9b81b977e20` |
| 產品版本與文件一致性 | `docs` 已對照 `VERSION` 驗證 Web、Extension、Flutter、CHANGELOG 與 Release Notes；Tag 一致性仍由 Release Workflow 驗證 |
| Go 後端 | `server-ci` 已執行格式、vet、單元測試、race test 與 build |
| Web | `web-ci` 已執行鎖定安裝、lint、typecheck、單元測試與 production build |
| Extension | `extension-ci` 已驗證 Chrome／Edge Manifest V3、權限、秘密掃描與個別 ZIP 封裝 |
| Flutter 基線 | Ubuntu 已通過 analyze、test 與 Android Debug build；Windows 已通過 analyze、test 與 Windows Release build |
| PostgreSQL 與端對端資料流 | `integration-test` 已驗證 migration、登入與 Token 輪替、設備、傳輸、分段續傳、WebSocket、權限、清理、重啟與故障恢復 |
| 容量目標 | `integration-test` 已執行 100 台註冊、50 台在線、10 筆並行傳輸與一般 API p95 小於 500 ms 的負載驗證 |
| 容器基線 | `docker-build` 已建置測試映像並通過啟動與健康檢查 |
| 安全基線 | `security` 已執行 CodeQL、Go／npm／Dart 相依掃描、秘密掃描、容器掃描與授權檢查，並產生 CycloneDX SBOM |
| 文件 | `docs` 已驗證 Markdown、相對連結、版本、套件 manifest 與簽章 Secrets 契約 |

上述 CI 證據集中於 [PR #15](https://github.com/Honguan/NexDrop/pull/15)。PR 保留全部必要檢查成功的紀錄；Release Workflow 仍須針對正式 Tag 重新執行發布驗證。

## 支援平台與建置版本

| 產物 | 版本與平台 |
| --- | --- |
| NexDrop Node | 產品 `1.0.0`；Linux amd64／arm64；Go 1.26.5；Alpine 3.24.1；PostgreSQL 17 |
| NexDrop Desktop | 產品 `1.0.0`；Windows 10／11 x64；Flutter stable |
| NexDrop Android | 產品 `1.0.0`；Android；application ID `io.github.honguan.nexdrop`；Flutter stable；JVM 工具鏈 17 |
| NexDrop Web | 產品 `1.0.0`；現代桌面與手機瀏覽器；Node.js 24 建置 |
| NexDrop Extension | 產品 `1.0.0`；Chrome／Edge；Manifest V3；Node.js 24 建置 |
| 公開契約 | API v1；傳輸協議 1.1；最低用戶端版本 1.0 |

## 尚待正式發布驗證

| 項目 | 完成條件 |
| --- | --- |
| Android 正式產物 | 注入受保護 Environment 的四項 Android Secrets，建置 Release APK 並通過 `apksigner` 正式簽章驗證 |
| Windows 正式產物 | 注入受保護 Environment 的 PFX 與密碼，完成 EXE／ZIP 簽章、安裝、啟動、解除安裝與 Authenticode 驗證 |
| Linux Node 正式壓縮檔 | Release Workflow 建置 amd64／arm64、解壓並驗證二進位格式與 migration 完整性 |
| 多架構容器 | 推送 `1.0.0`、`1.0`、`latest`，驗證 digest、健康狀態、Artifact Attestation 與 OIDC Cosign 簽章 |
| Release 完整性 | 產生並重算 SHA-256、SPDX SBOM、Artifact Attestation，確認所有產物可追溯至同一 Commit |
| Git Tag 與草稿 | 合併後建立不可變 `v1.0.0` Tag；Release Workflow 只能建立草稿，經人工核准後才可公開 |

目前不得將 `1.0.0` 標記為正式完成。待辦項目完成後，發布人員應在 GitHub Actions、GHCR 與草稿 Release 中逐一核對產物名稱、簽章、雜湊、SBOM、Commit 與 Tag，再公開 Stable Release。

## 必要外部設定

`release` Environment 必須禁止管理員略過人工核准，並設定下列 Secrets：

- `ANDROID_KEYSTORE_BASE64`
- `ANDROID_STORE_PASSWORD`
- `ANDROID_KEY_ALIAS`
- `ANDROID_KEY_PASSWORD`
- `WINDOWS_CERTIFICATE_BASE64`
- `WINDOWS_CERTIFICATE_PASSWORD`

不得以測試金鑰、debug key 或未簽章產物取代正式驗證。
