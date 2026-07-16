# Node 傳輸協議

Node 透過 HTTPS API 建立任務、上傳加密分段、完成組裝並由目標下載。新版變更操作帶 Idempotency-Key；分段以索引與 SHA-256 去重。Node 強制配額、到期與權限，磁碟不足時拒絕新 Node 檔案，但不阻止文字與 LAN 直傳。
