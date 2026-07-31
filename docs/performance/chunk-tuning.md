# Adaptive chunk and stream tuning

The `adaptive_transfer_profile` capability allows sender and receiver to negotiate safe limits and adjust chunk size or parallelism only between completed chunks.

## Inputs

- round-trip time and smoothed throughput
- retry and checksum-failure rates
- receiver memory and write capacity
- storage backpressure
- battery, thermal, and background constraints

## Bounds

The default range is 1-32 MiB per chunk and 1-6 concurrent chunks. The active profile must satisfy:

```text
chunk_size * parallel_chunks <= receiver_memory_budget
```

Small files use one stream. High latency, retries, checksum failures, or device pressure reduce chunk size and concurrency. Fast, stable LAN measurements increase them gradually. The algorithm uses bounded step changes to avoid oscillation.

## Baseline profiles

- unstable or high-latency mobile link: 1-2 MiB, one stream
- ordinary Wi-Fi: 4-8 MiB, one to three streams
- fast LAN: 16-32 MiB, up to six streams when memory permits

## Validation matrix

Benchmark 1 MiB, 100 MiB, 1 GiB, and multi-gigabyte files at 10/100/250/500 ms latency and 0/1/5/20 percent packet loss. Record selected profile, throughput, retries, checksum failures, memory, storage backpressure, and UI responsiveness.

## Compatibility

Without mutual capability support, NexDrop keeps the fixed negotiated chunk size and a single transfer stream. Existing chunk hashes and resumability remain valid in both modes.
