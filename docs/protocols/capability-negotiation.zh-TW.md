# 能力協商

[English](capability-negotiation.md)

NexDrop 以協議版本拒絕不相容的線上格式，並以能力識別碼啟用可選行為；產品行銷版本不決定功能是否可用。

## 節點文件

`GET /api/version` 除既有產品與協議欄位外，另回傳：

```json
{
  "nodeIdentity": "node-<opaque fingerprint>",
  "capabilitySchemaVersion": 1,
  "versionFingerprint": "<sha256>",
  "capabilities": ["capability_negotiation", "structured_errors"],
  "limits": {
    "maxChunkSize": 8388608,
    "maxParallelChunks": 3,
    "maxRecipients": 100
  }
}
```

用戶端依 `nodeIdentity` 隔離快取；每次開始工作階段時重新取得文件，若 `versionFingerprint` 改變就取代舊快取。舊節點未回傳這些欄位時，視為不支援任何可選能力。

用戶端必須忽略未知的新增欄位與能力識別碼。數值限制與布林能力分開協商，實際值不得超過所有必要參與方公布的限制。

## 工作階段與區網協商

新版用戶端以 WebSocket `capabilities` 查詢參數送出逗號分隔的能力清單；`connected` 訊息中的 `negotiatedCapabilities` 是與節點登錄表的交集。

LAN 用戶端以 `X-NexDrop-Capabilities` 送出同一份清單；接收端在續傳狀態回應中回傳協商結果與限制。只有下表列出的所有必要參與方都公布能力時，才能啟用功能。

| 能力 | 必要參與方 | 安全降級 |
| --- | --- | --- |
| `capability_negotiation` | 節點、用戶端 | 使用協議版本與最低用戶端版本檢查。 |
| `structured_errors` | 節點、用戶端 | 解析舊版字串錯誤格式。 |
| `cursor_pagination` | 節點、用戶端 | 使用舊版列表回應。 |
| `idempotency_replay` | 節點、用戶端 | 不自動重送非冪等請求。 |
| `resumable_chunks` | 發送端、接收端、節點 | 從第一個分段重新傳輸檔案。 |
| `realtime_versions` | 節點、用戶端 | 定期重新取得 HTTP 版本文件。 |

若要求的行為沒有安全降級方式，發起端須回傳或顯示 `CAPABILITY_UNAVAILABLE`，並在結構化詳細資料指出缺少的能力。

## 生命週期規則

- 識別碼使用小寫語意名稱，移除後不得改作其他用途。
- 新增能力須向後相容；移除前須完成文件化棄用期。
- 安全性重大不相容仍使用 `minimumClientVersion`。
- 協議相關變更必須同步更新[相容性矩陣](../compatibility-matrix.zh-TW.md)、本登錄表、契約 fixture 與混合版本測試。
