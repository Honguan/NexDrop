# Node 與命令列程式

[English](README.md)

`cmd/nexdrop` 是 Linux Node；`cmd/nexdrop-desktop-service` 提供 Windows 本機整合；`cmd/nexdrop-bridge` 是瀏覽器 Native Messaging 主機。Go 版本需求以英文主文件為準。

```bash
go build ./cmd/nexdrop
go test ./cmd/... ./internal/...
NEXDROP_DATABASE_URL='postgres://...' go run ./cmd/nexdrop serve
```

Node 支援 `version`、`status`、`doctor`、`backup`、`restore`、`cleanup`、`reset-password`。服務模式需要 PostgreSQL、migration、可寫入的檔案儲存目錄及已建置 Web 靜態檔；未指定輸出位置時，產物位於目前目錄或 `bin/`。
