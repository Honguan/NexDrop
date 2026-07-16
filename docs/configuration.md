# 設定參考

| 變數 | 必要 | 預設 | 用途 |
|---|---:|---|---|
| `POSTGRES_PASSWORD` | 是 | 無 | Compose PostgreSQL 密碼 |
| `NEXDROP_IMAGE` | 否 | `ghcr.io/honguan/nexdrop:1.0.0` | Node 映像完整標籤 |
| `NEXDROP_DOMAIN` | 是 | `localhost` | Caddy HTTPS 網域 |
| `NEXDROP_DATABASE_URL` | 容器內是 | Compose 自動設定 | PostgreSQL URL |
| `NEXDROP_STORAGE_PATH` | 否 | `/var/lib/nexdrop` | 加密分段與備份目錄 |
| `NEXDROP_WEB_PATH` | 否 | `/usr/share/nexdrop/web` | Web 靜態檔 |
| `NEXDROP_MIGRATIONS_PATH` | 否 | `/usr/share/nexdrop/migrations` | Migration 目錄 |
| `NEXDROP_BOOTSTRAP_ADMIN_*` | 首次啟動 | 空 | 首位管理員帳號、信箱、密碼 |
| `NEXDROP_LOGIN_RATE_LIMIT_PER_MINUTE` | 否 | `10` | 每 IP 與識別值登入上限 |
| `NEXDROP_PAIRING_RATE_LIMIT_PER_MINUTE` | 否 | `10` | 每 IP 與配對身分上限 |
| `NEXDROP_ADMIN_RATE_LIMIT_PER_MINUTE` | 否 | `30` | 每 IP 與管理工作階段上限 |

`.env` 權限應限制為部署管理員可讀。變更密碼、網域或儲存路徑後執行 `docker compose config` 再重啟；不得把真實值提交到 Git。
