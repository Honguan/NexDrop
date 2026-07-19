# NexDrop 2.0.1

## 修正內容

- 修正從 1.x 升級至 2.0 時，舊版 `.env` 沒有節點密鑰與 Web OTP 密鑰，造成更新後顯示空白的問題。
- `./deploy/nexdrop update` 現在會自動偵測缺少、空白或仍為範例值的 `NEXDROP_NODE_KEY` 與 `NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET`。
- 系統會產生安全隨機的 64 字元節點密鑰及 Base32 Web OTP 密鑰，寫入 `.env` 並在更新完成後顯示。
- 已存在的密鑰會完整保留，不會在日後更新時重新產生。
- 新產生的 Web OTP 密鑰只會套用至尚未啟用 OTP 的管理員，不會覆寫既有 OTP。
- 修正 Node 執行檔的版本注入，`nexdrop version`、健康狀態與正式映像會一致顯示 `2.0.1`。

## 更新方式

先取得最新版部署檔，再執行更新：

```bash
git pull
./deploy/nexdrop update 2.0.1
```

更新後可再次查看密鑰：

```bash
./deploy/nexdrop credentials --show-secrets
```

正常情況下會顯示：

```text
節點密鑰：<自動產生的密鑰>
Web OTP 密鑰：<自動產生的 Base32 密鑰>
```

## 驗證

- 模擬舊版 `.env` 完全沒有兩個新欄位的更新流程。
- 驗證首次更新會自動產生並輸出密鑰。
- 驗證再次更新會保留原密鑰且不重新產生。
- 驗證 Node 執行檔實際輸出的版本與 `VERSION` 完全一致。
- 完整執行部署流程、伺服器、Web、Flutter、Docker、整合及安全測試。
