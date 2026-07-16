# 資料模型

核心關係：User 擁有 Device；Group 擁有 Member 與 Device；TransferTask 對每個 Device 建立 TransferTarget；File 對每個目標建立 FileTarget 並由 FileChunk 組成；Message、ContentKey、DeliveryStatus、Metric、Notification 與 AuditLog 皆連回任務或裝置。

`transfer_tasks.idempotency_key` 與 sender device 唯一，`request_fingerprint` 用於偵測 key 重用；檔案分段以 `(file_id, chunk_index)` 唯一；統計以 `event_id` 唯一。`transfer_executions` 保留每個目標的執行階段，不覆寫終止歷史。

傳輸狀態只允許 domain 定義的 v1.3 矩陣。終止狀態不可回到執行中；送達只能轉為已讀。所有狀態更新在 PostgreSQL row lock 交易內完成。
