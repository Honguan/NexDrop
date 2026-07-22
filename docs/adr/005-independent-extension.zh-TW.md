# ADR-005：獨立瀏覽器擴充設備

[English](005-independent-extension.md)

狀態：已接受

背景：要求 Windows 用戶端持續執行，會讓行動裝置或非 Windows 瀏覽器無法使用 Extension，也會混淆兩個獨立安裝用戶端的設備身分。

決策：每個 Chrome 或 Edge Extension 都登記為獨立 Web 設備，直接向設定的 HTTPS Node 驗證，在擴充功能本機儲存區保留自己的 X25519 金鑰與工作階段，且只要求該 Node 的網站存取權限。Extension 不再依賴 Native Messaging 或桌面程序。

影響：Extension 與桌面安裝會顯示為不同設備，並可獨立撤銷。Extension 不提供 LAN 檔案直傳，完整檔案流程交由 NexDrop Web。
