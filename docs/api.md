# API v1（NexDrop 2.0）

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

遇到 `RATE_LIMITED` 時，新版錯誤訊息會要求依 `Retry-After` 回應標頭等待後再試；用戶端不得立即連續重送。

建立傳輸、上傳分段、完成檔案、同步統計、回報進度及已讀須送 `Idempotency-Key: <UUID>`。相同 key 與內容會重播原結果；不同內容回傳 `IDEMPOTENCY_CONFLICT`。

`GET /api/transfers` 新版支援 `limit`（1–100）、`cursor`、`from`、`to`、`status`，依 `created_at DESC, id DESC` 回傳。管理端 `GET /api/admin/failures` 與 `GET /api/admin/audit-logs` 在協商新版媒體型別時使用相同的分頁參數與 envelope；未協商的 1.x 用戶端仍可使用既有 `offset` 陣列介面。

```json
{"items":[],"nextCursor":"<opaque signed cursor>"}
```

游標以 HMAC 綁定 UTC 建立時間與穩定 UUID 排序鍵；用戶端不得解析或修改，簽章不符會回傳 `INVALID_PAGE`。管理端失敗列表的 `status` 篩選目標狀態，稽核列表則以 `status` 篩選稽核動作。

主要資源包含 auth、account、devices、groups、transfers、files、metrics、statistics 與 admin。`GET /api/version` 回傳產品、API、協議、最低用戶端與建置 Commit。

## 節點加入與裝置狀態

一般設備先以 `POST /api/node/enroll` 提交安裝器輸出的節點密鑰，取得節點擁有者的裝置工作階段；接著呼叫 `POST /api/devices` 建立自己的加密裝置身分。新裝置會直接建立為 `TRUSTED`，並將目前工作階段連結至該裝置。NexDrop 2.0 不再公開裝置配對碼、待核准或人工核准端點。

節點密鑰只用於一般設備加入，不取代 Web 管理後台的帳號、密碼與六位數 TOTP。建議透過 `./deploy/nexdrop credentials` 複製完整節點匯入 JSON，避免手動輸入錯誤。

`GET /api/devices` 在既有欄位之外回傳 `online` 與可選的 `lastSeenAt`。`online` 代表裝置仍有未中斷連線，且最近 45 秒內送出過心跳；用戶端應以此欄位顯示即時狀態，不應只依 `trustStatus` 推斷在線狀態。

`GET /api/statistics/devices` 每台裝置回傳 `deviceType`、`trustStatus`、`online`、可選的 `lastSeenAt`，以及傳送／接收筆數、位元組與平均速率。`GET /api/statistics/node` 仍接受 `from`、`to` 時間範圍；第一方介面只查詢最近兩分鐘並顯示最新節點取樣。節點資源每 5 秒取樣，高頻樣本保留 31 天。

群組 API 暫時保留供舊資料與 API v1 相容；NexDrop 2.0 第一方 Web、Windows、Android 與擴充功能統一使用節點聊天室，不再提供群組入口。
