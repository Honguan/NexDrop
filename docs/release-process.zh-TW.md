# 發布流程

[English](release-process.md)

## 一鍵發布

在 GitHub Actions 開啟 `release-package`，選擇 `patch`、`minor` 或 `major`，填寫英文與繁體中文更新摘要後按下 **Run workflow**。流程會自動：

1. 同步 `VERSION`、所有套件版本、Windows 資源版本、CHANGELOG 與 Release Notes。
2. 建立或沿用唯一的 `release/v<版本>` PR。
3. 主動執行 integration、server、web、extension、flutter、docker、security、docs 必要檢查。
4. 必要時以 rebase 更新落後的版本分支；所有必要檢查成功後才自動 squash merge。
5. 從最新 `master` 建立不可變 `v<版本>` Tag，執行並等待 `release` Workflow 完成。

若任何必要檢查失敗，流程會保留版本 PR 及檢查紀錄，不建立 Tag。修正同一個 PR 後重新執行 `release-package` 即可續跑；不會建立重複 PR。若版本 PR 在檢查期間再次落後 `master`，流程最多自動同步並重驗三次。

若版本 PR 已合併但 Tag 或 Release 尚未完成，重新執行會同時檢查 Tag、成功的 Release Workflow 與實際 Release，直接從同一個 `master` Commit 恢復，不會再遞增版本。若該 Commit 已有執行中的 Release，流程會接手等待而不會重複啟動。

## 本機準備版本

需要先在本機檢查或自訂版本時可執行：

```powershell
./scripts/prepare-release.ps1 -Bump patch -Summary '本版更新摘要'
./scripts/check-docs.ps1
```

提交版本 PR 後，既有 `publish-on-version` 流程仍會在 `VERSION` 合併至 `master` 時建立 Tag 並啟動正式發布，作為一鍵流程以外的相容入口。

## 依賴更新 PR

Dependabot 每週一 03:00（Asia/Taipei）把 Go、Web、Extension、Flutter 與 GitHub Actions 的一般版本更新合併為一個 `nexdrop-dependencies` PR。該 PR 會自動 rebase，且只有在全部必要檢查成功後才透過 GitHub Auto-merge 整合；工作流不再依任一個 `check_suite` 成功事件直接合併。

GitHub 目前只支援跨生態系統合併一般版本更新，緊急安全更新仍可能由 Dependabot 建立獨立 PR。這類 PR 不應為了維持數量而關閉；完成安全修補後，下一輪一般更新仍只會有一個整合 PR。

## 正式產物

Release Workflow 建置 Node amd64/arm64、Windows EXE/ZIP、Android APK、Chrome/Edge ZIP 與 GHCR 多架構映像，並驗證 SHA-256、SBOM、Artifact Attestation、容器映像與各平台產物後建立 Release。

正式 Tag 不得重寫。修補版本應建立新的 PATCH Tag。容器正式部署建議固定完整版本；`latest` 僅供一般使用者方便。

## 個人專案簽章策略

Android 與 Windows 的程式碼簽章為可選設定，不再阻止建立 Tag 或 Release：

- 六項簽章 Secrets 全部設定時，Workflow 會建立並驗證正式簽章產物。
- 未設定 Android 簽章 Secrets 時，Workflow 會建立具 v1/v2 臨時簽章、可在 Android 6.0 以上安裝的 APK；未設定 Windows 憑證時仍建立未簽章 EXE 與 ZIP。
- 臨時 Android 簽章每次發布可能不同，因此後續更新可能需要先移除舊版；要直接覆蓋更新，必須安全保存固定 keystore 並設定四項 Android Secrets。Windows 未簽章產物則可能顯示 SmartScreen 警告。
- GHCR 映像仍使用 GitHub OIDC、Cosign、Artifact Attestation、SBOM 與 SHA-256 驗證，不需要長期簽章私鑰。

需要正式平台簽章時，可在 Repository Settings → Environments 建立 `release` Environment，並選擇性加入：

| Secret | 格式與用途 |
| --- | --- |
| `ANDROID_KEYSTORE_BASE64` | Android JKS/PKCS12 keystore 的單行 Base64 |
| `ANDROID_STORE_PASSWORD` | keystore 密碼 |
| `ANDROID_KEY_ALIAS` | Android 簽章金鑰別名 |
| `ANDROID_KEY_PASSWORD` | Android 簽章金鑰密碼 |
| `WINDOWS_CERTIFICATE_BASE64` | 含私鑰之 Windows PFX 的單行 Base64 |
| `WINDOWS_CERTIFICATE_PASSWORD` | PFX 密碼 |

個人專案不必設定必要審查者；是否啟用 Environment 人工核准由維護者自行決定。
