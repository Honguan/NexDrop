# ADR-004：Web 與桌面橋接

[English](004-desktop-bridge.md)

狀態：已由 [ADR-005](005-independent-extension.zh-TW.md) 取代

背景：原始瀏覽器擴充功能需要把頁面內容交給已登入的桌面用戶端。

決策：原始 Extension 透過 Native Messaging 呼叫 `nexdrop-bridge`，再連到只監聽本機且使用短期權杖的桌面服務。

影響：該設計需為 Chrome/Edge 個別註冊原生主機。現行 Extension 已不再使用此橋接。
