# 離線送達協議

NexDrop 將在線狀態、信任、中繼資料同步、檔案本體下載、送達與已讀分開處理。

## 優先順序

1. 緊急中繼資料與文字
2. 連結、圖片與小型檔案
3. 延後的大型檔案與資料夾

檔案本體前先同步中繼資料。裝置重新連線後，先處理尚未同步的中繼資料，再依優先級與較小檔案排序。

## 狀態

`QUEUED`、`METADATA_AVAILABLE`、`WAITING_FOR_DEVICE`、`WAITING_FOR_NETWORK_POLICY`、`WAITING_FOR_POWER`、`DOWNLOADING`、`PAUSED_BY_SYSTEM`、`DELIVERED`、`READ`、`EXPIRED`、`CANCELLED`。

等待或暫停狀態附帶穩定原因碼，例如 `WIFI_REQUIRED`、`CHARGING_REQUIRED`、`MOBILE_DATA_LIMIT`、`FOREGROUND_REQUIRED`、`INSUFFICIENT_STORAGE`、`SYSTEM_RESOURCE_LIMIT`。

## 確認語意

- Node 已接受不等於已送達
- 中繼資料已同步不等於本體已下載
- 本體下載必須完成驗證
- 已讀狀態由每個裝置獨立維護

佇列變更與通知都必須冪等，重新連線與重試不得產生重複通知。

## 隱私

推播預設只包含不透明識別碼。只有使用者明確開啟預覽時才包含訊息文字或檔名。
