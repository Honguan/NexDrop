# 效能驗證

第一版容量情境為 100 台已註冊設備、50 台同時在線與 10 筆並行檔案傳輸；一般 API（不含檔案內容與大型統計）p95 必須低於 500 ms。

部署隔離測試 Node 與 PostgreSQL、建立測試帳號及裝置後，以 10 個並行工作執行：

```bash
go run ./cmd/nexdrop-loadtest -url http://127.0.0.1:8080 -username load-admin -password '<test-only-password>' -setup-scenario -devices 100 -online 50 -transfers 10 -requests 1000 -concurrency 10 -max-p95 500ms -environment '<CPU and memory>' -postgres '<PostgreSQL version and resources>' -report load-verification-report.json
```

工具會透過正式登入與設備 API 建立 100 個各自綁定的設備工作階段並完成核准、將其中 50 台連上 WebSocket、建立 10 筆未完成檔案傳輸，再於連線維持期間量測一般 API。JSON 報告保存產品版本、Commit、CPU/記憶體環境、PostgreSQL 規格、裝置／連線／傳輸數、成功率與 p50/p95。只可在設有 `NEXDROP_LOGIN_RATE_LIMIT_PER_MINUTE=200` 的拋棄式隔離環境執行，不得對正式資料執行；Integration Workflow 會保存報告 14 天。
