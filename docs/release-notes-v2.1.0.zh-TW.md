# NexDrop 2.1.0

[English](release-notes-v2.1.0.md)

NexDrop 2.1.0 新增明確的能力協商，讓節點、Windows、Android、Web、Chrome 與 Edge 用戶端能更安全地混合版本運作。

## 重點

- 版本端點現在會公布穩定的節點身分、能力結構版本、限制與版本指紋。
- HTTP、WebSocket 與 LAN 工作階段只會在各方共同支援後啟用可選功能。
- 目前用戶端連線至舊節點時，會安全降級至協議 1.1 或 1.0。
- LAN 探索維持相容的 1.1 基線，驗證後的傳輸再協商協議 1.2 功能。
- 續傳分段必須由傳輸兩端共同支援，否則會安全地從頭開始傳輸。
- 相容性錯誤會指出缺少的能力，以及需要更新的是節點或目標設備。
- 正式協議契約變更時，CI 會偵測未同步更新的相容性文件。

## 更新

```bash
./deploy/nexdrop update 2.1.0
```

更新會保留 `.env`、PostgreSQL 資料、檔案資料、既有秘密與 `NEXDROP_NODE_ID`，並在切換映像前建立備份。

若 Release 使用臨時 Android 簽章，已安裝舊版 APK 的裝置可能需要先移除舊版再安裝；正式環境應設定固定 Android 簽章 Secrets。未提供 Windows 憑證時，EXE 與 ZIP 仍可使用，但 Windows 可能顯示 SmartScreen 警告。
