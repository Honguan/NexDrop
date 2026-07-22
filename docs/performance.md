# Performance validation

[繁體中文](performance.zh-TW.md)

The v1 capacity scenario covers 100 registered devices, 50 concurrently online devices, and 10 concurrent file transfers. Ordinary interface requests, excluding file bodies and large statistics queries, must remain below 500 ms at p95.

In an isolated deployment containing the Node and PostgreSQL, create test accounts and devices, then run ten workers:

```bash
go run ./cmd/nexdrop-loadtest -url http://127.0.0.1:8080 -username load-admin -password '<test-only-password>' -setup-scenario -devices 100 -online 50 -transfers 10 -requests 1000 -concurrency 10 -max-p95 500ms -environment '<CPU and memory>' -postgres '<PostgreSQL version and resources>' -report load-verification-report.json
```

The tool uses production login and device endpoints to create 100 independently bound device sessions and approve them. It keeps 50 WebSocket connections open, creates 10 unfinished file transfers, and measures ordinary requests while those connections remain active. The JSON report records product version, commit, CPU and memory environment, PostgreSQL resources, device, connection, and transfer counts, success rate, and p50/p95 latency. Run it only in a disposable isolated environment with `NEXDROP_LOGIN_RATE_LIMIT_PER_MINUTE=200`, never against production data. The Integration workflow retains the report for 14 days.
