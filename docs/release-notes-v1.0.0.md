# NexDrop 1.0.0

首個穩定版提供 Windows、Android、Web、Chrome/Edge 與自架 Linux Node 的混合式加密傳輸。

主要功能、安全修正與已知問題見 [CHANGELOG](https://github.com/Honguan/NexDrop/blob/v1.0.0/CHANGELOG.md)。升級前必須備份；由預覽版升級時 Node 會依序套用 migration。若需回滾，應停止服務並還原升級前備份，不可只降級映像。

升級既有部署時請使用 `./deploy/nexdrop update`；若 `.env` 尚無 `NEXDROP_CURSOR_SECRET`，腳本會以系統亂數安全產生並持久保存。直接使用 Compose 的部署者須先加入至少 32 字元的穩定秘密，後續不得任意更換，否則既有分頁游標會失效。

相容性：API v1、協議 1.1，最低用戶端 1.0。正式產物附於本 Release，請先以 `checksums-sha256.txt`、GitHub Artifact Attestation 與平台簽章驗證。

已知問題：Android 與 Windows 正式產物只有在簽章祕密齊備時建立；內部 CA 憑證需預先加入用戶端信任庫。
