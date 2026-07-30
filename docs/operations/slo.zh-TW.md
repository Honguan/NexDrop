# 服務等級目標

[English](slo.md)

下列初始目標適用於健康且受支援的 NexDrop 節點，不含排定維護。量測遵循[可觀測性契約](transfer-events.zh-TW.md)的有限標籤規則，原始資源識別碼不得作為指標標籤。

| 目標 | 門檻 | 量測方式 |
| --- | --- | --- |
| API 可用性 | 滾動 30 天達 99.9% | 排除有效 4xx 後的非管理 API 成功回應 |
| 一般 API 延遲 | p95 小於 500 ms | 已發布的 100 台註冊、50 台在線、10 筆傳輸負載情境 |
| 在線文字送達 | 99% 於 30 秒內 | 目標持續在線時，從 `TASK_CREATED` 至 `TARGET_DELIVERED` |
| 可續傳性 | 99% 且不重複完成 | 中斷後可續傳或安全重啟的合格傳輸 |
| 狀態一致性 | 99.99% | 任務、目標、execution 與送達狀態一致的傳輸 |

`GET /metrics` 提供 Prometheus 文字格式。操作指標只允許 `route`、`status`、`error_code`、`worker`、`operation` 與 `result`；不在文件允許清單內的值會被拒絕，或正規化為 `OTHER`／`UNKNOWN`。目前涵蓋 API 結果與延遲、路由選擇、直連握手延遲與失敗原因、WebSocket 連線事件與心跳缺口、分段重試、校驗與儲存拒絕結果、送達延遲、路由吞吐量、資料庫與儲存延遲、Worker 執行與復原結果、傳輸狀態事件，以及目前排隊、停滯、已送達未確認與 dead-letter 數量。

可用性或一致性未達標時應立即調查。延遲或續傳未達標時，先查看傳輸時間軸、建立[診斷包](diagnostics.zh-TW.md)，再依[疑難排解](../troubleshooting.zh-TW.md)的穩定錯誤碼處置。
