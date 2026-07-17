# 發布流程

1. 更新 `VERSION`、各套件版本、CHANGELOG、Release Notes 與相容性資訊。
2. 確認 integration 已以 PostgreSQL 17 驗證遷移、服務重啟、HTTP／WebSocket 與跨使用者資料流，且 server、web、extension、flutter、docker、security、docs 全部通過。
3. 配置受保護 `release` environment、Android keystore 與 Windows PFX Secrets；Android、Windows 與 GHCR 發布工作本身必須綁定此 environment。
4. 建立不可變 `v<VERSION>` Tag。
5. Release Workflow 建置 Node amd64/arm64、Windows EXE/ZIP、Android APK、Chrome/Edge ZIP 與 GHCR 多架構映像。
6. 驗證簽章、SHA-256、SBOM/attestation、容器啟動及升級說明後，由授權人員發布草稿。

正式 Tag 不得重寫。修補使用新 PATCH。容器正式部署固定完整版本；`latest` 僅提供一般使用者方便。若 migration 不支援舊 schema，回滾必須還原升級前備份。

## 簽章 Secrets 契約

以下 Secrets 必須建立於受保護的 GitHub `release` Environment，不得建立於儲存庫檔案、一般 CI 產物或 Fork Pull Request：

| Secret | 格式與用途 |
| --- | --- |
| `ANDROID_KEYSTORE_BASE64` | 正式 Android JKS/PKCS12 keystore 的單行 Base64 |
| `ANDROID_STORE_PASSWORD` | keystore 密碼 |
| `ANDROID_KEY_ALIAS` | 正式簽章金鑰別名 |
| `ANDROID_KEY_PASSWORD` | 正式簽章金鑰密碼 |
| `WINDOWS_CERTIFICATE_BASE64` | 含私鑰之 Windows PFX 的單行 Base64 |
| `WINDOWS_CERTIFICATE_PASSWORD` | PFX 密碼 |

Android keystore 與 Windows PFX 只在受保護 Runner 的暫存目錄解碼，工作結束時刪除。GHCR 與 Cosign 使用 GitHub 短期權杖及 OIDC，不另建立長期 Registry 或 Cosign 私鑰 Secret。環境管理員加入或輪替 Secrets 後，先確認必要審查者仍生效，再建立正式 Tag；不得為了測試發布而提交測試 keystore、PFX 或密碼。
