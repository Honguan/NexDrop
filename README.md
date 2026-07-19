# NexDrop

NexDrop 2.0 是可自行架設的多設備加密聊天室與檔案傳輸平台，支援 Windows、Android、Web、Chrome 與 Edge。設備只需「節點連結＋節點密鑰」即可加入；傳送、接收與歷史紀錄統一顯示在聊天室。

## 主要能力

- 預設向節點所有設備廣播文字、網址、圖片與檔案，也可指定私人接收設備。
- 私人內容只對傳送設備及指定設備可見。
- 顯示傳送設備、時間、在線狀態與送達／失敗狀態。
- Web、Windows、Android 支援拖放圖片與檔案。
- 新設備加入與收到新內容時顯示通知。
- 管理後台可刪除離線設備，並以帳號、密碼及六位數 OTP 登入。
- 區網優先，無法直連時由自行部署的 Linux Node 接力。

## 快速部署 Linux Node

需求：Docker Engine 24+、Docker Compose v2。

```bash
git clone https://github.com/Honguan/NexDrop.git
cd NexDrop
./deploy/nexdrop install
```

安裝器預設偵測伺服器 IP，完成後輸出：

- 節點 IP 連結
- 節點密鑰
- 一鍵匯入 JSON
- Web 管理員帳號、密碼
- OTP 設定密鑰、目前六位數 OTP 與 Authenticator URI

要改用網域，先設定 DNS A／AAAA 記錄，再執行 `./deploy/nexdrop configure` 填入 `https://你的網域`。可同時設定只允許特定來源 IP。

## 常用命令

```bash
./deploy/nexdrop credentials
./deploy/nexdrop configure
./deploy/nexdrop status
./deploy/nexdrop logs nexdrop
./deploy/nexdrop backup --output /var/lib/nexdrop/backups/manual.tar.gz
./deploy/nexdrop update
# 鎖定本版
./deploy/nexdrop update 2.0.0
```

## 開發與文件

```bash
go test ./...
cd web && npm ci && npm test && npm run build
cd ../extension && npm ci && npm test && npm run build
cd ../client && flutter analyze && flutter test
```

完整設定見 [設定文件](docs/configuration.md)，升級與變更見 [CHANGELOG.md](CHANGELOG.md) 與 [2.0.0 發布說明](docs/release-notes-v2.0.0.md)。

本專案採 [MIT License](LICENSE)。
