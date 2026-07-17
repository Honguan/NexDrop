# 部署、升級與還原

## 首次部署

執行 `./deploy/nexdrop install`，依精靈確認網域與管理員資料，並保存首次顯示的隨機管理員密碼。精靈會自動產生 PostgreSQL 密碼及游標秘密，等待所有容器健康後才完成。以 `docker compose ps`、`/healthz` 與 `/readyz` 驗證服務。

## 升級

1. 閱讀 CHANGELOG 與 Release Notes。
2. 執行 `./deploy/nexdrop update <VERSION>`；腳本會先備份，再切換完整版本映像並等待健康檢查。
3. 執行 `./deploy/nexdrop doctor`，並確認 PostgreSQL 密碼與 `NEXDROP_CURSOR_SECRET` 未改變。

Migration 在 Node 啟動時依編號執行。更新失敗時腳本會停止 Node、恢復舊映像設定並保留更新前備份；包含不可逆 schema migration 的版本不可只降級映像，應先還原升級前備份，再啟動舊版。不要直接刪除 PostgreSQL 或檔案資料卷。

## 備份與還原

備份同時包含資料庫、節點金鑰與檔案；輸出應加密並移出主機。還原前停止 Node，使用 `./deploy/nexdrop restore <備份>`，完成後執行 doctor 並抽查傳輸下載。
