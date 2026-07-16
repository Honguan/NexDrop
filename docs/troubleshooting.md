# 故障排除

1. 執行 `docker compose config`、`docker compose ps`、`./deploy/nexdrop doctor`。
2. 使用 `./deploy/nexdrop logs nexdrop` 依 request ID 或 transfer ID 搜尋，不要公開完整日誌中的環境資訊。
3. `/healthz` 失敗表示程序不可用；`/readyz` 失敗通常是 PostgreSQL 連線或 migration 問題。
4. 上傳被拒絕時檢查單檔、使用者、群組、每日與節點磁碟配額。
5. LAN 無法直傳時檢查同網段、用戶端在線、防火牆、mDNS/UDP 與 AP isolation；Node 後援不應受影響。
6. `RATE_LIMITED` 請依 `Retry-After` 等待，不要持續重送。
7. SHA-256 不符時刪除本次未完成分段並由來源檔重新建立任務；來源檔變更不可沿用舊任務。

升級失敗先保留資料卷與備份，不要執行 `docker compose down --volumes`。
