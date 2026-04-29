---
name: hbase-metrics
description: Use when diagnosing HBase cluster health/performance — RPC latency, GC, hotspots, compaction backlog, blockcache hit rate, WAL slowness, master state. Queries VictoriaMetrics via hbase-metrics-cli and returns structured JSON for analysis.
---

# HBase Metrics Skill

## When to use
- User asks about HBase being slow, RPC latency high, single-RS hotspot, GC pressure, compaction backlog, read/write skew, blockcache thrashing, WAL slow appends, or "show me HBase status".
- User wants a current health check on an HBase cluster.

## Pre-flight check (run first)
1. `hbase-metrics-cli config show` — confirm VM URL and default cluster.
2. If `vm_url` source is `default`, prompt the user to run `hbase-metrics-cli config init` (or set `HBASE_VM_URL`).

## Twelve scenarios

| Scenario | When to use | Example |
|---|---|---|
| `cluster-overview` | First glance | `hbase-metrics-cli cluster-overview --since 24h --format json` |
| `regionserver-list` | RS distribution | `hbase-metrics-cli regionserver-list --format table` |
| `requests-qps` | QPS trend | `hbase-metrics-cli requests-qps --since 30m` |
| `rpc-latency` | "RPC slow" | `hbase-metrics-cli rpc-latency --top 10 --since 24h` |
| `handler-queue` | "Stuck calls" | `hbase-metrics-cli handler-queue` |
| `hotspot-detect` | "One RS hot" | `hbase-metrics-cli hotspot-detect --top 5` |
| `gc-pressure` | "GC heavy" | `hbase-metrics-cli gc-pressure --since 24h` |
| `jvm-memory` | Heap close to max | `hbase-metrics-cli jvm-memory` |
| `compaction-status` | Compaction backlog | `hbase-metrics-cli compaction-status --since 1h` |
| `blockcache-hitrate` | Reads slow / cache miss | `hbase-metrics-cli blockcache-hitrate` |
| `wal-stats` | Writes slow | `hbase-metrics-cli wal-stats --since 30m` |
| `master-status` | Master / RIT issues | `hbase-metrics-cli master-status` |

## Common flags
`--cluster X` `--since 5m|1h|24h` `--step auto|30s|...` `--raw` `--top N` `--format json|table|markdown` (default `json`) `--dry-run`

## Reading summary mode

When `--since` is set, hybrid and range scenarios return `mode: "summary"`. Each row aggregates one instance (or one label value) over the window with `max`, `avg`, `p99`, `last`. Prefer `max` for hotspot detection, `p99` for tail-latency trends, `avg` for sustained load, `last` for the freshest value.

Pass `--raw` only when you need the full datapoint matrix (rare — typically for plotting).

## Output contract
- **stdout** = JSON envelope `{scenario, cluster, mode, range?, queries[].expr, columns, data[]}` (`mode` ∈ `instant` | `summary` | `raw`)
- **stderr** = structured errors `{error:{code, message, hint}}`
- **exit codes**: `0` success or NoData (warning on stderr) / `1` internal / `2` user error / `3` VM failure

## Diagnostic playbook — "HBase is slow"
Run in this order rather than going straight to one metric; later steps interpret earlier ones.

1. `cluster-overview` — overall severity & whether multiple RS are unhealthy
2. `rpc-latency` + `handler-queue` — service-side bottlenecks
3. `hotspot-detect` — single-RS hot spot driving the symptom
4. `gc-pressure` + `jvm-memory` — JVM dragging the RS
5. `compaction-status` + `blockcache-hitrate` — storage layer pressure
6. Drill in via `queries[].expr` (rerun with adjusted PromQL through `hbase-metrics-cli query '...'`)

## Escape hatch
For any case the 12 scenarios don't cover:

```bash
hbase-metrics-cli query 'sum by (instance) (rate(hadoop_hbase_totalrequestcount{cluster="mrs-hbase-oline"}[5m]))'
```

## Common errors
| Code | Action |
|---|---|
| `CONFIG_MISSING` | run `hbase-metrics-cli config init` |
| `VM_UNREACHABLE` | check VPN / DNS to `vm_url`, raise `--timeout` |
| `VM_HTTP_4XX` 401/403 | set `HBASE_VM_USER` / `HBASE_VM_PASS` |
| `NO_DATA` | confirm `--cluster` matches an active label, widen `--since` |
| `FLAG_INVALID` (`--since` rejected) | the scenario is purely instant (no `instant_summary`) — drop the flag or use a hybrid/range scenario |
