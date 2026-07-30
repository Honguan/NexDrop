# ADR-006: Content-free observability and read-only diagnostics

[繁體中文](006-observability-and-diagnostics.zh-TW.md)

Status: accepted

## Context

Operators must correlate a transfer across HTTP, WebSocket, workers, and storage without exposing message content, plaintext filenames, credentials, or encryption material. Metrics must remain safe for Prometheus cardinality, and support bundles must be reproducible without changing Node state.

## Decision

- Store an append-only, content-free transfer timeline ordered by UTC time and database sequence.
- Use stable event codes and request, transfer, file, target, execution, route, status, and error correlation fields.
- Allow clients to append only documented client-observed phases through an authenticated, idempotent endpoint. Server lifecycle events remain server-owned.
- Export operational metrics with fixed names and bounded label names and values. Raw resource identifiers are never labels.
- Generate diagnostic ZIP files through a read-only inspection path. Configuration uses a strict public-key allowlist; known secrets and credential-shaped text are redacted before compression.
- Run destructive fault injection only when the environment explicitly identifies disposable infrastructure.

## Consequences

The timeline can diagnose ordering and recovery without becoming a content audit log. New event codes and metric label values require contract and bilingual documentation updates. Diagnostic additions must pass decompressed-content leak tests, and fault scenarios must remain deterministic and repeatable.
