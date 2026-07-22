# 設定參考

| 變數 | 必要 | 預設 | 用途 |
|---|---:|---|---|
| `POSTGRES_PASSWORD` | 是 | install 隨機產生 | 至少 16 字元；支援英數及 `. _ ~ ! @ % + , : / -`，密碼不再插入資料庫 URL |
| `NEXDROP_CURSOR_SECRET` | 是 | 無 | 至少 32 字元，用於簽署歷史分頁游標；升級後須保持不變 |
| `NEXDROP_IMAGE` | 否 | `ghcr.io/honguan/nexdrop:2.0.2` | Node 映像完整標籤 |
| `NEXDROP_DOMAIN` | 是 | `localhost` | Caddy HTTPS 網域 |
| `NEXDROP_DATABASE_URL` | 容器內是 | Compose 自動設定 | 不含密碼的 PostgreSQL URL |
| `NEXDROP_DATABASE_PASSWORD` | 容器內是 | Compose 自動設定 | 原樣傳入的 PostgreSQL 密碼，避免特殊字元被當成 URL 語法 |
| `NEXDROP_STORAGE_PATH` | 否 | `/var/lib/nexdrop` | 加密分段與備份目錄 |
| `NEXDROP_WEB_PATH` | 否 | `/usr/share/nexdrop/web` | Web 靜態檔 |
| `NEXDROP_MIGRATIONS_PATH` | 否 | `/usr/share/nexdrop/migrations` | Migration 目錄 |
| `NEXDROP_BOOTSTRAP_ADMIN_*` | 首次啟動 | install 產生 | 首位管理員帳號、信箱與至少 12 字元密碼；密碼支援英數及 `. _ ~ ! @ % + , : / -` |
| `NEXDROP_LOGIN_RATE_LIMIT_PER_MINUTE` | 否 | `10` | 每 IP 與識別值登入上限 |

`./deploy/nexdrop install` 會顯示全部安全預設，並提供接受、逐項修改或全部重新隨機產生三種選項。`credentials` 預設只顯示管理員的 Bootstrap 初始登入資料；若密碼已重設，該初始值即失效。只有明確執行 `credentials --show-secrets` 才會輸出 PostgreSQL 密碼與游標秘密。`configure` 更新公開／bootstrap 設定；`configure-secrets` 則逐項詢問是否更新或重新隨機產生目前管理員密碼、PostgreSQL 密碼與游標秘密，並在資料庫命令成功後才改寫 `.env`。更換游標秘密會使既有游標失效。`.env` 權限限制為部署使用者可讀且不得提交到 Git。
