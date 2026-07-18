# NexDrop 1.0.2

此修補版改善 Linux Node 的安裝、密碼輪替與版本更新流程，讓 Chrome／Edge 擴充功能成為可獨立配對的設備，並修復 Samsung J6 將未簽章 APK 判定為「應用程式套件無效」的問題。

安裝精靈現在會自動取得 Docker 所需的管理員權限，同時保留 `.env` 的原使用者擁有權。所有隨機預設都會在安裝時顯示並可逐項修改；既有部署可使用 `./deploy/nexdrop credentials` 查看 Bootstrap 初始登入資料，以 `./deploy/nexdrop configure-secrets` 個別輪替管理員、PostgreSQL 與游標秘密。PostgreSQL 密碼已與 URL 分離，因此 `@`、`:`、`/` 等文件列出的特殊字元不再破壞連線字串。

執行 `./deploy/nexdrop update` 會自動查詢最新正式版本、建立更新前備份，再把 `.env` 鎖定至明確版本；也可執行 `./deploy/nexdrop update 1.0.2` 指定版本。更新會保留資料卷與既有秘密。

瀏覽器擴充功能不再依賴 Windows 桌面橋接。它會以獨立設備登入及等待管理員核准，小視窗可輸入內容、選擇特定接收設備，並記住是否附上目前分頁網址。限流錯誤會依 `Retry-After` 顯示實際等待時間。

Android APK 現在必須通過 v1/v2 簽章驗證。若發布環境未配置固定正式 keystore，Workflow 會產生可安裝的臨時簽章 APK，因此 Samsung J6 可正常安裝；但下個臨時簽章可能不同，屆時可能需要先移除舊版。要讓後續版本直接覆蓋更新，維護者仍應設定並安全備份固定 Android keystore。

相容性：API v1、協議 1.1，最低用戶端 1.0。正式產物附有 `checksums-sha256.txt`、SPDX SBOM、Artifact Attestation 與 GHCR Cosign 簽章。Windows 未設定憑證時，EXE 與 ZIP 仍可能顯示 SmartScreen 警告。
