# HTTP 介面 v3 整合

v3 整合端點延伸既有穩定的 v1 HTTP 契約，沿用相同的 Bearer Access Token、媒體類型、結構化錯誤、能力協商與 `Idempotency-Key` 規則。

## 憑證邊界

一般 API、WebSocket 與傳輸請求不會要求或傳送節點密鑰。

節點密鑰只允許用於裝置註冊 Bootstrap 與簽發註冊權杖。Bootstrap 成功後會換成單一裝置專用憑證，第一方客戶端會立即刪除節點密鑰。裝置憑證只用於把已驗證的使用者 Session 綁定到裝置；一般請求仍使用短效 Access Token。

## 裝置註冊

- `POST /api/v3/enrollment/bootstrap`
- `POST /api/v3/enrollment/grants`
- `POST /api/v3/enrollment/redeem`
- `POST /api/v3/enrollment/attach-session`
- `POST /api/v3/enrollment/grants/{grant-id}/revoke`
- `POST /api/v3/devices/{device-id}/credential/revoke`

Bootstrap 與權杖簽發需要 `X-NexDrop-Node-Key`。兌換端點有速率限制，並以單一 PostgreSQL 交易完成權杖驗證、使用次數消耗、裝置建立、X25519 公鑰登錄與裝置憑證建立。

## 自適應路由與傳輸設定

- `POST /api/v3/routes/plan`
- `POST /api/v3/routes/observations`
- `POST /api/v3/transfers/{transfer-id}/route`
- `POST /api/v3/transfers/{transfer-id}/profile`

路由規劃可接受已驗證的直連、VPN、IPv4、IPv6 與 Node 候選路徑，回應包含可重現的分數與啟動延遲。直連路徑具有短暫優先時間，但 Node fallback 不必等到完整直連逾時。切換路由會更新同一筆 Transfer 並保存已驗證分段雜湊，不會建立替代 Transfer。

自適應設定會依 RTT、吞吐量、重試率、校驗失敗率、接收端記憶體、背壓、儲存空間、電池、溫度與背景狀態，回傳有上下限的分段大小與並行數。

## 離線傳送

- `PUT /api/v3/transfers/{transfer-id}/targets/{device-id}/delivery-policy`
- `GET /api/v3/transfers/{transfer-id}/targets/{device-id}/delivery-policy`
- `POST /api/v3/transfers/{transfer-id}/targets/{device-id}/delivery-evaluate`
- `GET /api/v3/devices/{device-id}/delivery-queue`

政策包含到期時間、僅 Wi-Fi、僅充電、行動數據上限、自動下載與僅前景執行。裝置重新連線後會先同步中繼資料、高優先與小型項目，再處理延後的大型檔案內容。

## Relay Pool

- `POST /api/v3/relays`
- `GET /api/v3/relays`
- `POST /api/v3/relays/{relay-id}/heartbeat`
- `POST /api/v3/relays/{relay-id}/drain`
- `DELETE /api/v3/relays/{relay-id}`
- `POST /api/v3/relay-assignments`

註冊、列表、Drain 與移除需要近期完成二次驗證的管理員 Session。Relay 憑證與節點、裝置、管理員及資料庫憑證完全分離。分派結果會提供短效 Grant，綁定 Relay、Transfer、檔案、操作、位元組上限與到期時間。

## 資料夾 Manifest

- `PUT /api/v3/transfers/{transfer-id}/folder-manifest`
- `GET /api/v3/transfers/{transfer-id}/folder-manifest`
- `PUT /api/v3/transfers/{transfer-id}/folder-selection/{device-id}`
- `GET /api/v3/transfers/{transfer-id}/folder-selection/{device-id}`

傳送端上傳經驗證的版本化 Manifest，每個接收裝置分別保存接受項目與衝突處理選項。資料持久化前會檢查路徑正規化、目錄穿越、絕對路徑、磁碟代號、NUL、保留名稱、大小寫碰撞、深度、長度、項目數與 Manifest 大小。

## 訊息生命週期

- `GET /api/v3/messages?conversation=inbox&limit=50&cursor=...`
- `PUT /api/v3/messages/read`
- `DELETE /api/v3/messages/{message-id}?scope=LOCAL|ATTACHMENT_BODY|EVERYWHERE`
- `PUT /api/v3/retention/{conversation}`
- `POST /api/v3/retention/run`

歷史紀錄使用簽章穩定游標，不會暴露明文內容。已讀游標按裝置獨立保存。本機移除只影響發出請求的裝置；全域刪除會建立具簽章與版本的 Tombstone，讓離線裝置重新連線後同步。附件與訊息保留政策由有限批次、可重啟的清理 Worker 分開評估。

## Recovery 與死信操作

- `GET /api/v3/recovery/failed`
- `GET /api/v3/recovery/transfers/{transfer-id}`
- `POST /api/v3/recovery/transfers/{transfer-id}/retry`
- `POST /api/v3/recovery/run`

這些端點需要近期完成二次驗證的管理員 Session。Recovery 使用 PostgreSQL 租約、有限指數退避、逐目標重試狀態、檔案系統校正、追加式時間軸事件與穩定終止錯誤碼。

部署端等價指令：

```text
./deploy/nexdrop transfers failed
./deploy/nexdrop transfers inspect <transfer-id>
./deploy/nexdrop transfers retry <transfer-id>
./deploy/nexdrop transfers reconcile
```
