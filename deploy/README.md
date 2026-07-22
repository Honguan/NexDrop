# 部署與維運

需求為 Linux、Docker Engine 24+ 與 Compose v2。首次安裝直接執行：

```bash
./deploy/nexdrop install
./deploy/nexdrop status
./deploy/nexdrop doctor
```

安裝精靈會說明每個條件，建立權限為 `600` 的 `.env`，先產生安全隨機預設，再讓操作者接受、更新公開／管理員設定，或重新產生秘密。自動化部署使用 `./deploy/nexdrop install --non-interactive`；之後可執行 `./deploy/nexdrop configure` 更新網域與首次管理員設定。既有管理員密碼可用 `read -rsp '新密碼: ' P; echo; printf '%s\n' "$P" | ./deploy/nexdrop reset-password --identifier admin; unset P` 更新，避免密碼寫進 shell 歷史，也避免誤以為修改 bootstrap 設定會變更既有帳號。

正式環境以完整版本標籤固定映像。執行 `./deploy/nexdrop update 2.0.3` 會先建立資料庫與檔案備份，再切換 `NEXDROP_IMAGE`、等待健康檢查；失敗時停止 Node 並恢復舊映像設定，不會在未知 migration 狀態下自動降級啟動。更新不會重新產生 PostgreSQL 密碼、管理員密碼或 `NEXDROP_CURSOR_SECRET`。備份必須另存至受保護位置並定期還原演練。

PostgreSQL 密碼至少 16 字元，且因 Compose 連線 URL 限制，只能使用英數、點、底線、波浪號或連字號；建議使用 `openssl rand -hex 32`。管理員名稱為 3–64 字元、電子郵件須含 `@`、密碼至少 12 字元。Caddy 網域只填主機名稱，不包含 `https://`。
