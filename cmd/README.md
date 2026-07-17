# Node 與命令列

`cmd/nexdrop` 是 Linux Node；`cmd/nexdrop-desktop-service` 提供 Windows 本機整合；`cmd/nexdrop-bridge` 是瀏覽器 Native Messaging 主機。需求為 Go 1.26.5+。

```bash
go build ./cmd/nexdrop
go test ./cmd/... ./internal/...
NEXDROP_DATABASE_URL='postgres://...' go run ./cmd/nexdrop serve
```

Node 支援 `version`、`status`、`doctor`、`backup`、`restore`、`cleanup`、`reset-password`。服務模式依賴 PostgreSQL、migration、檔案儲存目錄及已建置 Web 靜態檔；產物預設位於目前目錄或 `bin/`。
