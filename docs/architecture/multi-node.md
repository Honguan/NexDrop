# Multi-node architecture

## Phase 1: primary control Node and secondary data relays

The primary Node remains authoritative for users, devices, authorization, transfer tasks, wrapped content keys, and relay assignment. Secondary relays store and serve only encrypted chunks.

A relay is registered with an identity key, endpoint, region, capacity, and health state. The primary Node selects a healthy, non-draining relay using available capacity, failure rate, latency, and preferred region. Clients receive a signed short-lived grant; relays never receive Node, administrator, database, or device credentials.

Relay lifecycle:

```text
REGISTERED -> HEALTHY -> DRAINING -> REMOVED
                 |          |
                 +-> UNHEALTHY
```

Draining stops new assignments while active verified chunks remain available. Expired grants and orphaned chunks are reconciled and removed by bounded cleanup jobs.

## Availability targets

Single-Node deployments require no extra configuration. With at least two relays, new assignments avoid an unhealthy or full relay and active transfers may resume through another authorized endpoint using verified chunks.

## Phase 2: standby control Node

Future control-plane failover requires PostgreSQL replication and promotion, stable Node identity, metadata recovery, client or DNS endpoint failover, and documented RPO/RTO. Active-active control Nodes remain out of scope until relay consistency and recovery are validated.
