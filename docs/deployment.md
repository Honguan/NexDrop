# 部署、升級與還原

## 首次部署

執行 `./deploy/nexdrop install` 即可；Docker 權限不足時腳本會自動透過 `sudo` 重新執行，無須先手動加上 `sudo`。精靈會顯示所有隨機預設，並讓使用者逐項修改管理員、PostgreSQL 密碼與游標秘密，等待所有容器健康後才完成。`./deploy/nexdrop credentials` 可重看 Bootstrap 初始登入資料；若已重設密碼，請使用新密碼，系統無法還原目前密碼。基礎設施秘密須明確加上 `--show-secrets` 才顯示。

既有安裝要輪替秘密時執行 `./deploy/nexdrop configure-secrets`。精靈會分別詢問管理員、PostgreSQL 與游標秘密，可輸入新值、輸入 `r` 個別重新隨機產生或保留不變；PostgreSQL 密碼會同步修改資料庫角色，避免只改 `.env` 導致服務無法啟動。

## 升級

1. 閱讀 CHANGELOG 與 Release Notes。
2. 執行 `./deploy/nexdrop update` 自動查詢 GitHub 最新正式版本，或用 `./deploy/nexdrop update <VERSION>` 切換指定版本；腳本會先備份、將 `.env` 鎖定到明確版本並等待健康檢查。自動查詢需要 `curl`。
3. 執行 `./deploy/nexdrop doctor`，並確認 PostgreSQL 密碼與 `NEXDROP_CURSOR_SECRET` 未改變。

Migration 在 Node 啟動時依編號執行。更新失敗時腳本會停止 Node、恢復舊映像設定並保留更新前備份；包含不可逆 schema migration 的版本不可只降級映像，應先還原升級前備份，再啟動舊版。不要直接刪除 PostgreSQL 或檔案資料卷。

## 備份與還原

備份同時包含資料庫、節點金鑰與檔案；輸出應加密並移出主機。還原前停止 Node，使用 `./deploy/nexdrop restore <備份>`，完成後執行 doctor 並抽查傳輸下載。
