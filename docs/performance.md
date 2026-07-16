# 效能驗證

第一版容量情境為 100 台已註冊設備、50 台同時在線與 10 筆並行檔案傳輸；一般 API（不含檔案內容與大型統計）p95 必須低於 500 ms。

部署隔離測試 Node 與 PostgreSQL、建立測試帳號及裝置後，以 10 個並行工作執行：

```bash
go run ./cmd/nexdrop-loadtest -url https://nexdrop.example.com -requests 1000 -concurrency 10 -max-p95 500ms
```

報告必須保存版本、Commit、CPU/記憶體、PostgreSQL 規格、裝置／連線／傳輸數、成功率與 p50/p95。此工具驗證 API 延遲門檻；完整裝置與檔案情境由整合測試環境先建立，不應對正式資料執行。
