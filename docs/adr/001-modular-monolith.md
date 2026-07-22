# ADR-001: Modular monolith and PostgreSQL task table

[繁體中文](001-modular-monolith.zh-TW.md)

Status: Accepted

Context: The first version needs consistent transactions, recoverable workers, and simple self-hosted operations.

Decision: Use a modular Go monolith. PostgreSQL stores both domain data and task state; do not add Redis.

Consequences: Deployment and consistency remain simpler. Scale must first be handled with database locks, indexes, and worker contention controls.
