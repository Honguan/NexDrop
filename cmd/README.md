# Node and command-line programs

[繁體中文](README.zh-TW.md)

`cmd/nexdrop` is the Linux Node, `cmd/nexdrop-desktop-service` provides Windows local integration, and `cmd/nexdrop-bridge` is the browser Native Messaging host. Development requires Go 1.26.5+.

```bash
go build ./cmd/nexdrop
go test ./cmd/... ./internal/...
NEXDROP_DATABASE_URL='postgres://...' go run ./cmd/nexdrop serve
```

The Node provides `version`, `status`, `doctor`, `backup`, `restore`, `cleanup`, and `reset-password`. Server mode requires PostgreSQL, migrations, a writable file-storage directory, and built Web assets. Local artifacts are written to the current directory or `bin/` unless a build script selects another destination.
