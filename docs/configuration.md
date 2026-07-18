# 設定參考

| 變數 | 必要 | 預設 | 用途 |
|---|---:|---|---|
| `POSTGRES_PASSWORD` | 是 | install 隨機產生 | 至少 16 字元且只含 URL-safe 字元的 Compose PostgreSQL 密碼 |
| `NEXDROP_CURSOR_SECRET` | 是 | 無 | 至少 32 字元，用於簽署歷史分頁游標；升級後須保持不變 |
| `NEXDROP_IMAGE` | 否 | `ghcr.io/honguan/nexdrop:1.0.1` | Node 映像完整標籤 |
| `NEXDROP_DOMAIN` | 是 | `localhost` | Caddy HTTPS 網域 |
| `NEXDROP_DATABASE_URL` | 容器內是 | Compose 自動設定 | PostgreSQL URL |
| `NEXDROP_STORAGE_PATH` | 否 | `/var/lib/nexdrop` | 加密分段與備份目錄 |
| `NEXDROP_WEB_PATH` | 否 | `/usr/share/nexdrop/web` | Web 靜態檔 |
| `NEXDROP_MIGRATIONS_PATH` | 否 | `/usr/share/nexdrop/migrations` | Migration 目錄 |
| `NEXDROP_BOOTSTRAP_ADMIN_*` | 首次啟動 | install 產生 | 首位管理員帳號、信箱與至少 12 字元密碼 |
| `NEXDROP_LOGIN_RATE_LIMIT_PER_MINUTE` | 否 | `10` | 每 IP 與識別值登入上限 |
| `NEXDROP_PAIRING_RATE_LIMIT_PER_MINUTE` | 否 | `10` | 每 IP 與配對身分上限 |
| `NEXDROP_ADMIN_RATE_LIMIT_PER_MINUTE` | 否 | `30` | 每 IP 與管理工作階段上限 |

`./deploy/nexdrop install` 會建立安全預設並互動確認；`configure` 可更新網域與 bootstrap 設定。已建立的管理員密碼須用 `reset-password` 修改。`.env` 權限應限制為部署管理員可讀，且不得提交到 Git。版本更新必須保留 PostgreSQL 密碼與 `NEXDROP_CURSOR_SECRET`，避免資料庫驗證失敗或既有游標失效。
