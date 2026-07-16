# 部署與維運

需求為 Linux、Docker Engine 24+ 與 Compose v2。複製 `.env.example` 為 `.env`，設定網域、PostgreSQL 密碼與首次管理員資料後執行：

```bash
docker compose pull
./deploy/nexdrop install
./deploy/nexdrop status
./deploy/nexdrop doctor
```

正式環境以 `NEXDROP_IMAGE=ghcr.io/honguan/nexdrop:1.0.0` 固定版本。`deploy/nexdrop` 提供 update、logs、backup、restore、cleanup、reset-password 與 uninstall；備份必須另存至受保護位置並定期還原演練。Caddy 終結 TLS，Node、PostgreSQL 與資料卷留在 Compose 私有網路。
