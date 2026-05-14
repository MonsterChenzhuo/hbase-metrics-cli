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

The **Mode** column tells you whether `--since` is accepted:

- `range` — must use `--since` (defaults if omitted). Returns `mode: summary` or `raw`.
- `hybrid` — instant by default; pass `--since` to get a windowed `summary` over that period.

All 12 embedded scenarios are `range` or `hybrid` — every one accepts `--since`. (A purely-instant scenario, where `--since` is rejected with `FLAG_INVALID`, is still possible in the schema but no longer present in the bundled set.)

| Scenario | Mode | When to use | Example |
|---|---|---|---|
| `cluster-overview` | hybrid | First glance | `hbase-metrics-cli cluster-overview --since 24h --format json` |
| `regionserver-list` | hybrid | RS distribution | `hbase-metrics-cli regionserver-list --since 1h --format table` |
| `requests-qps` | range | QPS trend | `hbase-metrics-cli requests-qps --since 30m` |
| `rpc-latency` | range | "RPC slow" | `hbase-metrics-cli rpc-latency --top 10 --since 24h` |
| `handler-queue` | hybrid | "Stuck calls" | `hbase-metrics-cli handler-queue --since 1h` |
| `hotspot-detect` | hybrid | "One RS hot" | `hbase-metrics-cli hotspot-detect --top 5 --since 1h` |
| `gc-pressure` | range | "GC heavy" | `hbase-metrics-cli gc-pressure --since 24h` |
| `jvm-memory` | hybrid | Heap close to max | `hbase-metrics-cli jvm-memory --since 24h` |
| `compaction-status` | range | Compaction backlog | `hbase-metrics-cli compaction-status --since 1h` |
| `blockcache-hitrate` | hybrid | Reads slow / cache miss | `hbase-metrics-cli blockcache-hitrate --since 24h` |
| `wal-stats` | range | Writes slow | `hbase-metrics-cli wal-stats --since 30m` |
| `master-status` | hybrid | Master / RIT issues | `hbase-metrics-cli master-status` |

## Common flags
`--cluster X` `--since 5m|1h|24h` (range / hybrid only) `--step auto|30s|...` `--raw` `--top N` `--format json|table|markdown` (default `json`) `--dry-run`

## Reading summary mode

When `--since` is set, range and hybrid scenarios return `mode: "summary"`. Each row aggregates one instance (or one label value) over the window with `max`, `avg`, `p99`, `last`. Prefer `max` for hotspot detection, `p99` for tail-latency trends, `avg` for sustained load, `last` for the freshest value.

Pass `--raw` when you need exact datapoints around an alarm minute. Raw mode is flattened for agents:

- Per-instance scenarios: one row per `instance` + `timestamp`, with `time` (UTC RFC3339) and one numeric column per query label.
- Label-value scenarios: one row per `label` + `timestamp`, with `time` and `value`.
- Use `timestamp` for exact comparisons and `time` for human-readable reports.

## Output contract
- **stdout** = JSON envelope `{scenario, cluster, mode, range?, queries[].expr, columns, data[]}` (`mode` ∈ `instant` | `summary` | `raw`)
- **stderr** = structured errors `{error:{code, message, hint}}`
- **exit codes**: `0` success or NoData (warning on stderr) / `1` internal / `2` user error / `3` VM failure

## HostName alarms
Cloud alarms may report `HostName=node-...`, but HBase metric rows usually expose only `instance` (`IP:port`) plus labels such as `role`, `service`, `cluster`, and `sub`. Do not guess the HostName-to-IP mapping from row order. First run `labels <metric>` or `label-check <metric> hostname`; if hostname is missing, map it through Manager/CMDB/inventory, then inspect the matching `instance`.

## Diagnostic playbook — "HBase is slow"
Run in this order rather than going straight to one metric; later steps interpret earlier ones.

1. `cluster-overview --since 24h` — overall severity & whether multiple RS are unhealthy
2. `rpc-latency --since 24h` + `handler-queue --since 1h` — service-side bottlenecks
3. `hotspot-detect --since 1h` — single-RS hot spot driving the symptom
4. `gc-pressure --since 24h` + `jvm-memory --since 24h` — JVM dragging the RS
5. `compaction-status --since 24h` + `blockcache-hitrate --since 24h` — storage layer pressure
6. Drill in via `queries[].expr` (rerun with adjusted PromQL through `hbase-metrics-cli query '...'`)

For a 24h health check, batch all of the above with `--since 24h`; all bundled scenarios currently accept `--since`.

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
| `FLAG_INVALID` (`--since` rejected) | the scenario is `instant` mode (see table above) — drop the flag |
