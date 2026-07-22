# 區網發現協議

[English](lan-discovery.md)

用戶端以 mDNS/DNS-SD 宣告 NexDrop 裝置識別、協議版本與傳輸端點，不廣播 Token、私鑰或內容。接收者必須以已配對設備公鑰驗證身分；發現只代表路徑候選，不代表授權。跨 VLAN、AP isolation 或作業系統背景限制可能使發現失敗，此時改用 Node。
