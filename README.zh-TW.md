# NexDrop

[English](README.md)

NexDrop 是可自行架設的混合式多裝置傳輸平台。它會優先在區網直接傳輸，無法直連時改由私有 Node 暫存，支援 Windows 10/11、Android、Web、Chrome 與 Edge。

目前版本以根目錄 [VERSION](VERSION) 為準。主要能力包含節點密鑰快速加入、端對端加密文字與檔案、分段續傳、即時設備狀態與逐設備統計。節點維運改由部署命令執行，不提供 Web 管理後台。

## 架構

Go 模組化單體提供 HTTP 介面、WebSocket 在線狀態與傳輸工作；PostgreSQL 保存狀態；Flutter 共用 Windows／Android 用戶端；React 提供 Web 用戶端；Manifest V3 擴充功能會登記為獨立設備。詳見[架構文件](docs/architecture.zh-TW.md)。

## 快速部署 Linux Node

需求：Docker Engine 24+、Docker Compose v2，以及能解析到主機的網域名稱。

```bash
git clone https://github.com/Honguan/NexDrop.git
cd NexDrop
./deploy/nexdrop install
docker compose ps
```

安裝精靈會取得 Docker 所需權限、說明網域與密碼條件，並產生 PostgreSQL 密碼、游標秘密及管理員密碼。畫面會顯示全部預設值，讓操作者接受、逐項修改或重新隨機產生；`.env` 維持原執行使用者可讀的 `600` 權限。自動化環境可使用 `./deploy/nexdrop install --non-interactive`。

Caddy 預設開放 `80/tcp`、`443/tcp`、`443/udp`；Node 只在 Compose 網路使用 `8080/tcp`。本機原始碼映像可用 `docker compose build --pull nexdrop` 建置。

## 開發與建置

```bash
go test ./...
go build ./cmd/nexdrop
cd web && npm ci && npm test && npm run build
cd ../extension && npm ci && npm test && npm run build
cd ../client && flutter analyze && flutter test
```

- Node 與命令列：[cmd/README.zh-TW.md](cmd/README.zh-TW.md)
- Windows／Android：[client/README.zh-TW.md](client/README.zh-TW.md)
- Web：[web/README.zh-TW.md](web/README.zh-TW.md)
- 瀏覽器擴充功能：[extension/README.zh-TW.md](extension/README.zh-TW.md)
- 部署維運：[deploy/README.zh-TW.md](deploy/README.zh-TW.md)

## 設定與維運

安全範例位於 [.env.example](.env.example)，完整說明見[設定文件](docs/configuration.zh-TW.md)。常用命令：

```bash
./deploy/nexdrop status
./deploy/nexdrop info
./deploy/nexdrop credentials
./deploy/nexdrop configure
./deploy/nexdrop configure-secrets
./deploy/nexdrop logs nexdrop
./deploy/nexdrop doctor
./deploy/nexdrop backup --output /var/lib/nexdrop/backups/manual.tar.gz
./deploy/nexdrop cleanup --limit 100
./deploy/nexdrop update
# 鎖定版本：./deploy/nexdrop update <version>
```

## 安全與發布

正式環境必須使用 HTTPS、強密碼及固定完整映像版本，不可提交 `.env`、Token、私鑰、keystore 或簽章憑證。弱點請依 [SECURITY.zh-TW.md](SECURITY.zh-TW.md) 私下回報。

正式產物與 SHA-256 位於 [GitHub Releases](https://github.com/Honguan/NexDrop/releases)。維護者可在 GitHub Actions 執行 `release-package`，由單一流程建立版本 PR、執行必要檢查、自動整合、建立不可變 Tag 並等待正式產物。變更與發布方式請見 [CHANGELOG.md](CHANGELOG.md) 與[發布流程](docs/release-process.zh-TW.md)。

## 文件

[繁中文件索引](docs/README.zh-TW.md)包含部署、介面、安全、資料模型、協議與故障排除。參與開發前請閱讀 [CONTRIBUTING.zh-TW.md](CONTRIBUTING.zh-TW.md) 與 [CODE_OF_CONDUCT.zh-TW.md](CODE_OF_CONDUCT.zh-TW.md)。

本專案採 [MIT License](LICENSE)。
