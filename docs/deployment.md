# 部署、升級與還原

## 首次部署

依根目錄 README 建立 `.env`，固定 `NEXDROP_IMAGE` 完整版本後執行 `docker compose pull && docker compose up -d`。以 `docker compose ps`、`/healthz` 與 `/readyz` 驗證服務。

## 升級

1. 閱讀 CHANGELOG 與 Release Notes。
2. 執行 `./deploy/nexdrop backup --output /var/lib/nexdrop/backups/pre-update.tar.gz`。
3. 修改 `NEXDROP_IMAGE` 為目標完整版本。
4. 執行 `./deploy/nexdrop update` 與 `./deploy/nexdrop doctor`。

Migration 在 Node 啟動時依編號執行。包含不可逆 schema migration 的版本不可只降級映像；應停止服務、還原升級前備份，再啟動舊版。不要直接刪除 PostgreSQL 或檔案資料卷。

## 備份與還原

備份同時包含資料庫、節點金鑰與檔案；輸出應加密並移出主機。還原前停止 Node，使用 `./deploy/nexdrop restore <備份>`，完成後執行 doctor 並抽查傳輸下載。
