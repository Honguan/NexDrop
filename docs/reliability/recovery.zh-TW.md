# 傳輸復原

復原協調器會在 Node 啟動、有限週期排程及管理員明確要求時執行。每次掃描都可重入，只處理已到期、非終止且成功取得租約的目標。

## 政策

- 起始退避：5 秒
- 最大退避：30 分鐘
- 最大自動嘗試：8 次
- 預設租約：2 分鐘
- 決定性抖動由 transfer 與 target 識別碼產生

## 狀態校正

針對不明確工作，比對 PostgreSQL 狀態、已記錄分段雜湊、儲存物件、組裝檔案雜湊與接收端確認。缺少或不一致的分段會標記重傳；有效且已完成的分段會保留。

## 死信處理

重試耗盡或錯誤不可重試時，目標進入死信。管理員必須能查看傳輸、穩定錯誤碼、最後嘗試及處理指引，再決定重試或取消。

建議命令：

```text
./deploy/nexdrop transfers failed
./deploy/nexdrop transfers inspect <transfer-id>
./deploy/nexdrop transfers retry <transfer-id>
./deploy/nexdrop transfers reconcile
```

## 操作驗證

測試上傳與下載中 Node 重啟、資料庫中斷、接收端離線、分段遺失、組裝失敗、重複復原與儲存空間不足。檔案儲存不可用時，文字傳送仍必須可用。
