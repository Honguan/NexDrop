# 安全政策

[English](SECURITY.md)

## 支援版本

安全修補以最新穩定版為目標。

## 回報弱點

請使用 GitHub Security Advisory 的 **Report a vulnerability** 私下回報，不要建立公開 Issue。請包含受影響版本、重現步驟、影響與可行緩解方式；不得附上真實使用者資料、Token、私鑰或檔案內容。

維護者會在七日內確認收件，完成風險分級後提供修補與揭露時程。修補發布前請勿公開細節。

部署端應固定完整容器版本、驗證 Release SHA-256 與 attestation、使用 HTTPS，並定期測試備份還原。
