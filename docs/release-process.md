# 發布流程

1. 更新 `VERSION`、各套件版本、CHANGELOG、Release Notes 與相容性資訊。
2. 確認 integration、server、web、extension、flutter、docker、security、docs 全部通過。
3. 建立不可變 `v<VERSION>` Tag；推送 Tag 後自動執行 Release Workflow。
4. Workflow 建置 Node amd64/arm64、Windows EXE/ZIP、Android APK、Chrome/Edge ZIP 與 GHCR 多架構映像。
5. 驗證 SHA-256、SBOM、Artifact Attestation、容器映像與各平台產物後，發布草稿 Release。

正式 Tag 不得重寫。修補版本應建立新的 PATCH Tag。容器正式部署建議固定完整版本；`latest` 僅供一般使用者方便。

## 個人專案簽章策略

Android 與 Windows 的程式碼簽章為可選設定，不再阻止建立 Tag 或 Release：

- 六項簽章 Secrets 全部設定時，Workflow 會建立並驗證正式簽章產物。
- 未設定簽章 Secrets 時，仍會建立未簽章 APK、EXE 與 ZIP，並正常產生草稿 Release。
- 未簽章產物可能出現 Android 手動安裝警告或 Windows SmartScreen 警告，發布說明應明確標示。
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
