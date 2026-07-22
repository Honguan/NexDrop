# NexDrop 1.0.0 發布就緒證據

[English](release-readiness-v1.0.0.md)

本文件記錄 `1.0.0` 的驗證狀態。所有必要 CI 通過，且正式產物可由 Commit、Tag、雜湊與 Attestation 追溯後，即可建立 Stable Release。

## 已通過

| 項目 | 證據 |
| --- | --- |
| 交付變更合併 | PR #15 的必要檢查全部成功，並合併為 Commit `47c56fc3262cbe658d191fe6a7f6a9b81b977e20` |
| 產品版本與文件一致性 | PR #16 docs job 已對照 `VERSION` 驗證 Web、Extension、Flutter、CHANGELOG 與 Release Notes |
| Go 後端 | `server-ci` 已執行格式、vet、單元測試、race test 與 build |
| Web | `web-ci` 已執行鎖定安裝、lint、typecheck、單元測試與 production build |
| Extension | `extension-ci` 已驗證 Chrome／Edge Manifest V3、權限、秘密掃描與個別 ZIP 封裝 |
| Flutter 基線 | Ubuntu 已通過 analyze、test 與 Android Debug build；Windows 已通過 analyze、test 與 Windows Release build |
| PostgreSQL 與端對端資料流 | `integration-test` 已驗證 migration、登入、設備、傳輸、續傳、WebSocket、權限、清理、重啟與故障恢復 |
| 容器與安全基線 | 已驗證容器建置、健康檢查、相依掃描、秘密掃描、SBOM 與授權檢查 |
| 文件 | `docs` 已驗證 Markdown、相對連結、版本及套件 manifest |

## 發布條件

| 項目 | 完成條件 |
| --- | --- |
| Git Tag | 在 `master` 建立不可變 `v1.0.0` Tag |
| Release Workflow | Tag 推送後，所有建置工作成功 |
| Release 完整性 | 產生並重算 SHA-256、SPDX SBOM 與 Artifact Attestation |
| 容器 | 推送 `1.0.0`、`1.0`、`latest` 並記錄映像 digest |
| 草稿 Release | 檢查產物名稱、雜湊、Commit 與 Tag 後公開 |

## 簽章說明

Android keystore 與 Windows PFX 不再是發布阻塞條件：

- 已設定六項 Secrets：建立並驗證簽章版本。
- 未設定 Secrets：Android 建立具 v1/v2 臨時簽章的可安裝 APK，Windows 建立未簽章 EXE 與 ZIP；Release Notes 必須標示 Android 後續更新可能需先移除舊版，或 Windows 可能出現 SmartScreen 警告。
- GHCR 映像仍以 GitHub OIDC、Cosign、Artifact Attestation、SBOM 與 SHA-256 驗證。

因此，個人維護者不需要外部審查者，也可以建立 `v1.0.0` Tag 並完成發布。平台程式碼簽章可在日後取得憑證時補上，並以新的 PATCH 版本重新發布。
