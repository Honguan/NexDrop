# Adaptive route selection

NexDrop evaluates authenticated LAN, VPN, IPv4, IPv6, and Node candidates independently. Discovery supplies candidates only; it never grants trust.

## State flow

```text
DISCOVER -> AUTHENTICATE -> SCORE -> RACE -> SELECT -> TRANSFER
                         \-> REJECT
TRANSFER -> DEGRADED -> RACE -> MIGRATE -> TRANSFER
```

Direct candidates receive a configurable 350 ms head start. The Node candidate starts immediately when no usable direct route exists. Candidates are rejected when authentication fails, health is below the configured threshold, or the endpoint is incomplete.

## Scoring

The deterministic score considers route kind, recent handshake latency, measured throughput, failure rate, and recency of the last success. Equal scores are ordered by direct-route preference, route kind, and stable candidate identifier.

## Migration

A route change does not reset a file. The sender compares verified chunk hashes with chunks available on the replacement route and retransmits only missing or mismatched chunks. Transfer history records the selected route, fallback reason, and migration event.

## Compatibility

`adaptive_route_racing` must be mutually negotiated. Otherwise clients retain sequential LAN-first behavior and use the Node only after the direct attempt fails.

## Troubleshooting

- LAN discovered but unreachable: verify access-point isolation and host firewall rules.
- VPN route not selected: confirm the candidate is authenticated and has a recent successful measurement.
- IPv6 stalls: keep IPv4 and Node candidates enabled so the race can complete through another route.
- Frequent fallback: inspect route failure rate and handshake latency metrics before increasing the direct head start.
