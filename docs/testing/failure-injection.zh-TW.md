# 故障注入

[English](failure-injection.md)

故障注入只能在可丟棄環境執行。未設定 `NEXDROP_INTEGRATION_DISPOSABLE=true` 時，腳本會拒絕執行。

隔離的節點及 PostgreSQL 容器啟動後：

```sh
export NEXDROP_INTEGRATION_DISPOSABLE=true
export NEXDROP_POSTGRES_CONTAINER="$(docker compose ps -q postgres)"
./scripts/failure-injection.sh database-outage
```

先執行 `./scripts/failure-injection.sh deterministic`，驗證儲存連線重設、WebSocket 中斷與重連、狀態轉換及重播。設定 `NEXDROP_TEST_DATABASE_URL` 後會一併執行 PostgreSQL 併發重試重播測試；未設定時腳本會明確標示略過。資料庫情境會停止 PostgreSQL、確認 `/readyz` 回傳 503、重新啟動 PostgreSQL 並等待就緒。整合 Workflow 會在隔離的 service container 執行兩種模式，並另外在進行中傳輸期間重新啟動節點程序，確認已完成分段、冪等鍵及已儲存結果不會重複。

自動化確定性測試涵蓋連線重設、受控延遲、儲存拒絕與慢速、WebSocket 重連、能力降級、用戶端限制訊號、重試重播、資料庫不可用與節點重啟復原。真實封包整形、檔案系統耗盡、Android Doze 控制與實體介面切換是選用的設備實驗室延伸；不得對正式節點或永久 Volume 執行破壞性控制。

| 模式 | 可重複的注入條件與斷言 |
| --- | --- |
| `network-faults` | 上傳／下載重設、延遲送達／重連，以及 LAN 不可用時的 IPv4／IPv6 路由降級 |
| `storage-faults` | 慢速永久寫入、拒絕寫入後清理、校驗拒絕、配額與儲存空間不足拒絕 |
| `client-constraints` | 接收端背景限制與網路介面變更事件保持無內容且有序 |
| `mixed-clients` | 支援／不支援協議，以及舊版／新版結構化錯誤降級 |
| `worker-recovery` | Cleanup 重入、WebSocket 重連、併發重試重播與單一永久 execution |
| `database-outage` | PostgreSQL 中斷時就緒檢查安全失敗，重啟後復原 |
| 整合 Workflow 重啟屏障 | 進行中傳輸跨越節點重啟，且不重複分段或完成結果 |

網路模式使用可確定重現的程序內重設與路由可用性控制，不依賴特權封包整形，因此開發機與隔離 CI Runner 可執行相同測試。Android Doze／背景與介面變更以穩定的用戶端觀察事件表示；設備實驗室可再額外操作真實作業系統控制。

每個新增情境都必須定義故障點、預期狀態轉換、清理方式、重試鍵，並證明一筆邏輯請求最多產生一筆永久結果。
