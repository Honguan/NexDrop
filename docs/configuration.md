# 設定參考

| 變數 | 必要 | 預設 | 用途 |
|---|---:|---|---|
| `POSTGRES_PASSWORD` | 是 | install 隨機產生 | PostgreSQL 密碼 |
| `NEXDROP_CURSOR_SECRET` | 是 | install 隨機產生 | 歷史分頁游標簽章，升級時須保留 |
| `NEXDROP_IMAGE` | 否 | `ghcr.io/honguan/nexdrop:2.0.0` | Node 映像標籤 |
| `NEXDROP_NODE_URL` | 是 | `http://伺服器IP` | 顯示及匯入給設備的節點連結 |
| `NEXDROP_SITE_ADDRESS` | 是 | `:80` | Caddy 監聽位址；網域模式使用 `https://drop.example.com` |
| `NEXDROP_NODE_OWNER` | 是 | `admin` | 使用節點密鑰加入的設備所屬帳號 |
| `NEXDROP_NODE_KEY` | 是 | install 隨機產生 | 一般設備加入節點的唯一共享密鑰，至少 32 字元 |
| `NEXDROP_ALLOWED_IPS` | 否 | 空白 | 逗號分隔來源 IP 白名單；空白不限制 |
| `NEXDROP_BOOTSTRAP_ADMIN_USERNAME` | 首次啟動 | `admin` | Web 管理員帳號 |
| `NEXDROP_BOOTSTRAP_ADMIN_EMAIL` | 首次啟動 | `admin@example.com` | Web 管理員電子郵件 |
| `NEXDROP_BOOTSTRAP_ADMIN_PASSWORD` | 首次啟動 | install 隨機產生 | Web 管理員密碼 |
| `NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET` | 首次啟動 | install 隨機產生 | Web 管理員 Base32 OTP 密鑰 |
| `NEXDROP_LOGIN_RATE_LIMIT_PER_MINUTE` | 否 | `10` | 登入及節點加入限流 |
| `NEXDROP_ADMIN_RATE_LIMIT_PER_MINUTE` | 否 | `30` | 管理 API 限流 |

`./deploy/nexdrop install` 預設偵測伺服器 IP。安裝完成後會輸出設備一鍵匯入 JSON 及 Web 管理員 OTP 資料。使用網域時，先將 DNS A／AAAA 記錄指向伺服器，再執行 `./deploy/nexdrop configure` 將節點連結改為 HTTPS 網域。

一般設備只需節點連結與節點密鑰；Web 管理後台才需要帳號、密碼及六位數 OTP。`credentials` 會再次顯示上述資料；`credentials --show-secrets` 另外顯示 PostgreSQL 與游標秘密。
