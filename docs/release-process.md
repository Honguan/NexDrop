# 發布流程

1. 更新 `VERSION`、各套件版本、CHANGELOG、Release Notes 與相容性資訊。
2. 確認 server、web、extension、flutter、integration、docker、security、docs 全部通過。
3. 配置受保護 `release` environment、Android keystore 與 Windows PFX Secrets。
4. 建立不可變 `v<VERSION>` Tag。
5. Release Workflow 建置 Node amd64/arm64、Windows EXE/ZIP、Android APK、Chrome/Edge ZIP 與 GHCR 多架構映像。
6. 驗證簽章、SHA-256、SBOM/attestation、容器啟動及升級說明後，由授權人員發布草稿。

正式 Tag 不得重寫。修補使用新 PATCH。容器正式部署固定完整版本；`latest` 僅提供一般使用者方便。若 migration 不支援舊 schema，回滾必須還原升級前備份。
