# ADR-001：模組化單體與 PostgreSQL 任務表

[English](001-modular-monolith.md)

狀態：已接受

背景：第一版需要一致交易、可恢復 Worker 與簡單自架維運。

決策：Go 採模組化單體，PostgreSQL 同時保存業務資料與任務狀態，不引入 Redis。

影響：部署與一致性較簡單；擴展時必須先以資料庫鎖、索引與 Worker 競爭控制容量。
