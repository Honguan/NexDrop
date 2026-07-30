# 傳輸事件碼

[English](transfer-events.md)

`GET /api/transfers/{id}/timeline` 依 UTC 發生時間及單調遞增序號回傳事件，授權規則與傳輸資源相同。事件只含穩定事件碼，以及請求、傳輸、檔案、目標、execution、路由、狀態、錯誤與量測時間關聯欄位；不含訊息內容、檔名、加密材料或使用者檔案。

第一方用戶端透過 `POST /api/transfers/{id}/timeline` 回報下列用戶端觀察階段，並提供 UUID `Idempotency-Key`。相同鍵與內容會重播原事件；相同鍵搭配不同內容會回傳 `IDEMPOTENCY_CONFLICT`。用戶端不得提交由伺服器管理的生命週期事件。

| 事件碼 | 意義 | 操作員處置 |
| --- | --- | --- |
| `TASK_CREATED` | 傳輸交易已建立 | 確認目標及下一個路由事件 |
| `TARGET_RESOLVED` | 要求的目標已授權並解析 | 確認目標與選定路由 |
| `ROUTE_CHECKING` | 開始評估路由 | 檢查目標在線狀態與節點就緒狀態 |
| `TARGET_WAITING` | 目標、LAN 或節點相依項目不可用 | 檢查在線狀態、防火牆、DNS 與節點健康 |
| `TARGET_QUEUED` | 工作排隊中 | 檢查佇列成長及 Worker 健康 |
| `NODE_UPLOAD_STARTED` | 傳送端開始上傳節點 | 檢查儲存容量與寫入延遲 |
| `NODE_FILE_AVAILABLE` | 節點上傳完成 | 檢查接收端在線狀態與下載授權 |
| `NODE_DOWNLOAD_STARTED` | 接收端開始從節點下載 | 檢查接收端網路與儲存 |
| `LAN_TRANSFER_STARTED` | 開始 LAN 直連傳輸 | 檢查 TLS 身分與區域網路 |
| `TRANSFER_PAUSED` | 目前 execution 暫停 | 保留 execution，使用相同傳輸續傳 |
| `FILE_HASH_VERIFYING` | 開始驗證完整檔案雜湊 | 等待驗證；失敗時調查校驗碼 |
| `FILE_HASH_VERIFIED` | 完整檔案雜湊驗證成功 | 繼續送達，不需修復 |
| `TARGET_DELIVERED` | 目標已確認送達 | 除非確認延遲未達 SLO，否則不需處理 |
| `TARGET_READ` | 目標已標示讀取 | 不需處理 |
| `TARGET_FAILED` | 目前 execution 失敗 | 依 `errorCode` 處理後，以新 execution 重試 |
| `TARGET_CANCELLED` | 傳送端取消未完成目標 | 不自動重新啟動 |
| `TARGET_EXPIRED` | 保存期限到期 | 仍需要內容時建立新傳輸 |
| `SOURCE_FILE_MISSING` | 傳送端來源不存在 | 還原來源後明確重試 |
| `SOURCE_FILE_CHANGED` | 傳送端來源已變更 | 驗證新來源後建立新傳輸或明確重試 |
| `ROUTE_MIGRATED` | execution 已更換路由 | 檢查前一個路由或握手失敗原因 |
| `RETRY_STARTED` | 新的冪等 execution attempt 已開始 | 確認同一重試鍵只有一筆 execution |
| `ROUTE_CANDIDATES_DISCOVERED` | 用戶端完成路由探索 | 比對可用路由與最後選定路由 |
| `DIRECT_CONNECTION_ATTEMPTED` | 用戶端嘗試直連 | 未出現 TLS 事件時檢查 LAN 可達性 |
| `TLS_AUTHENTICATION_COMPLETED` | 雙向 TLS 驗證成功 | 繼續直連傳輸 |
| `ROUTE_FALLBACK_SELECTED` | 用戶端選定備援路由 | 檢查前一個路由或連線錯誤 |
| `ENCRYPTION_PREPARED` | 用戶端完成內容加密與金鑰包裝 | 不記錄金鑰材料並繼續傳輸 |
| `CHUNK_UPLOAD_STARTED` | 用戶端開始上傳節點分段 | 檢查節點儲存與請求關聯 |
| `CHUNK_DOWNLOAD_STARTED` | 用戶端開始下載節點分段 | 檢查接收端網路與授權 |
| `CHUNK_RETRY_STARTED` | 用戶端重試分段作業 | 檢查穩定錯誤碼與重試率 |
| `WEBSOCKET_INTERRUPTED` | 即時連線中斷 | 檢查網路與重連指標 |
| `RECEIVER_BACKGROUND_RESTRICTED` | 接收端回報背景執行限制 | 將接收端切至前景或調整系統設定 |
| `NETWORK_INTERFACE_CHANGED` | 用戶端網路介面變更 | 續傳前重新探索路由 |

穩定錯誤碼處置詳見[疑難排解](../troubleshooting.zh-TW.md)。
