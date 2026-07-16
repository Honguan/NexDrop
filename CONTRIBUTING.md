# 貢獻指南

1. 從 `master` 建立短期分支，只修改與議題直接相關的內容。
2. Go 執行 `gofmt`、`go vet ./...`、`go test ./...`；Web/Extension 執行 `npm ci`、lint、typecheck、test、build；Client 執行 `flutter analyze` 與 `flutter test`。
3. 資料庫變更只新增遞增編號 migration，不修改已發布 migration。
4. 公開 API 變更須更新 `docs/api.md`、相容性測試及 CHANGELOG。
5. PR 應描述行為、測試證據、遷移／回滾影響與已知限制；不可提交秘密或產物。

提交訊息使用 `feat:`、`fix:` 或 `chore:` 的簡短命令式摘要。
