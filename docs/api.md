# API v1

基礎路徑為 `/api`，時間使用 UTC RFC 3339，資源 ID 使用 UUID。所有回應包含：

```text
X-Request-ID: <UUID>
X-NexDrop-API-Version: 1
```

第一方用戶端送出 `Accept: application/vnd.nexdrop.v1+json`。新版錯誤格式：

```json
{"error":{"code":"INVALID_TOKEN","message":"...","request_id":"...","details":{}}}
```

未協商媒體型別的 1.x 舊用戶端仍取得 `{"error":"INVALID_TOKEN"}`。

建立傳輸、上傳分段、完成檔案、同步統計、回報進度及已讀須送 `Idempotency-Key: <UUID>`。相同 key 與內容會重播原結果；不同內容回傳 `IDEMPOTENCY_CONFLICT`。

`GET /api/transfers` 新版支援 `limit`（1–100）、`cursor`、`from`、`to`、`status`，依 `created_at DESC, id DESC` 回傳：

```json
{"items":[],"nextCursor":"<opaque signed cursor>"}
```

游標以 HMAC 綁定 UTC 建立時間與 UUID；用戶端不得解析或修改，簽章不符會回傳 `INVALID_PAGE`。

主要資源包含 auth、account、devices、groups、transfers、files、metrics、statistics 與 admin。`GET /api/version` 回傳產品、API、協議、最低用戶端與建置 Commit。
