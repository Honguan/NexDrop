# ADR-004：Web 與桌面橋接

狀態：已接受

背景：瀏覽器擴充功能需要把頁面內容交給已登入的桌面用戶端。

決策：Extension 透過 Native Messaging 呼叫 `nexdrop-bridge`，再連到只監聽本機且使用短期權杖的桌面服務。

影響：安裝時需為 Chrome/Edge 個別註冊原生主機；橋接檔與權杖不得對其他使用者開放。
