# 部署與維運

[English](README.md)

需求為 Linux、Docker Engine 24+ 與 Docker Compose v2。首次安裝執行：

```bash
./deploy/nexdrop install
./deploy/nexdrop status
./deploy/nexdrop info
./deploy/nexdrop doctor
```

安裝精靈會說明每個條件、建立權限為 `600` 的 `.env`、產生安全預設值，並讓操作者接受、修改或重新產生公開設定、管理員資料及秘密。自動化部署使用 `./deploy/nexdrop install --non-interactive`；之後可執行 `./deploy/nexdrop configure` 更新網域與 bootstrap 管理員欄位。

`./deploy/nexdrop info` 只顯示 Node 網址、設定映像、bootstrap 識別資料、設定檔路徑與原始碼版本，不會洩漏秘密。只有確實需要秘密時才執行 `credentials --show-secrets`。

修改 bootstrap 欄位不會變更既有帳號。請用下列方式重設既有管理員密碼，避免密碼寫入 shell 歷史：

```bash
read -rsp '新密碼: ' P; echo
printf '%s\n' "$P" | ./deploy/nexdrop reset-password --identifier admin
unset P
```

正式環境應固定完整映像版本。執行 `./deploy/nexdrop update <version>` 會先備份資料庫與檔案，再切換 `NEXDROP_IMAGE` 並等待健康檢查；失敗時停止 Node、恢復原映像設定，不會在 migration 狀態未知時自動以舊映像啟動。更新會保留 PostgreSQL 密碼、管理員密碼與 `NEXDROP_CURSOR_SECRET`。備份必須另存至受保護位置並定期演練還原。

PostgreSQL 密碼至少 16 字元，支援英文字母、數字及 `.`、`_`、`~`、`!`、`@`、`%`、`+`、`,`、`:`、`/`、`-`；Compose 會將密碼與資料庫 URL 分開傳入。建議使用 `openssl rand -hex 32` 產生安全預設。管理員名稱為 3–64 字元、電子郵件須含 `@`、密碼至少 12 字元，可使用相同字元集。Caddy 網域只填主機名稱，不包含 `https://`。
