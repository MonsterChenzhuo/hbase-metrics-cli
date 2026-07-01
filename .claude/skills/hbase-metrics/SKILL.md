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

## Fourteen scenarios

The **Mode** column tells you whether `--since` is accepted:

- `range` — must use `--since` (defaults if omitted). Returns `mode: summary` or `raw`.
- `hybrid` — instant by default; pass `--since` to get a windowed `summary` over that period.

All 14 embedded scenarios are `range` or `hybrid` — every one accepts `--since`. (A purely-instant scenario, where `--since` is rejected with `FLAG_INVALID`, is still possible in the schema but no longer present in the bundled set.)

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
| `storage-usage` | hybrid | StoreFile / MemStore bytes per RS | `hbase-metrics-cli storage-usage --format table` |
| `read-write-split` | hybrid | "Read-driven or write-driven?" | `hbase-metrics-cli read-write-split --since 10m --format table` |

## Common flags
`--cluster X` `--since 5m|1h|24h` (range / hybrid only) `--step auto|30s|...` `--raw` `--top N` `--format json|table|markdown` (default `json`) `--dry-run`

`query` only: `--end <unix|RFC3339|"2006-01-02 15:04:05">` pins an absolute past window end (default now); `--tz <IANA|+08:00>` sets the timezone `--end` is read in and adds a `time_local` column. **Alarms in this fleet are Beijing time (UTC+8); VM stores UTC** — pass `--tz Asia/Shanghai` so you stop hand-converting.

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

## Storage usage workflow
Use this when the user asks who is using HBase/HDFS space, table size, namespace size, StoreFile size, or "storage占用".

1. Start with metrics if the question is cluster/RegionServer level:
   `hbase-metrics-cli storage-usage --format table`
2. If the user asks for table or namespace size, first check whether the exporter has table/namespace labels:
   `hbase-metrics-cli labels hadoop_hbase_storefilesize --format json`
3. If `table` / `namespace` labels are absent, say metrics cannot answer table-level size from VM and switch to HDFS directory accounting:
   `hdfs dfs -du -s -h /hbase/data/<namespace>` and `hdfs dfs -du -s -h '/hbase/data/<namespace>/*'`
4. When unsure about metric names, use the agent-friendly discovery command before raw PromQL:
   `hbase-metrics-cli metrics size --format table`

## Diagnostic playbook — "HBase is slow"
Run in this order rather than going straight to one metric; later steps interpret earlier ones.

1. `cluster-overview --since 24h` — overall severity & whether multiple RS are unhealthy
2. `rpc-latency --since 24h` + `handler-queue --since 1h` — service-side bottlenecks
3. `read-write-split --since 1h` — is the load/latency read-driven or write-driven? (`write_read_ratio` >>1 + a `wal_sync_p99_ms` spike ⇒ write path; ratio <1 ⇒ go to blockcache/compaction below)
4. `hotspot-detect --since 1h` — single-RS hot spot driving the symptom
5. `gc-pressure --since 24h` + `jvm-memory --since 24h` — JVM dragging the RS
6. `compaction-status --since 24h` + `blockcache-hitrate --since 24h` + `storage-usage --since 24h` — storage layer pressure
7. Drill in via `queries[].expr` (rerun with adjusted PromQL through `hbase-metrics-cli query '...'`)

For a 24h health check, batch all of the above with `--since 24h`; all bundled scenarios currently accept `--since`.

## Escape hatch
For any case the 13 scenarios don't cover, first discover metric names with `metrics [contains]`, then query raw PromQL:

```bash
hbase-metrics-cli metrics request --format table

# Instant (mode: instant) — one row per series, current value:
hbase-metrics-cli query 'sum by (instance) (rate(hadoop_hbase_totalrequestcount{cluster="mrs-hbase-oline"}[5m]))'

# Range (mode: raw) — add --since (and optional --step) to get a time series
# you can scan for a peak/alarm minute. Emits columns [instance, timestamp, time, value]:
hbase-metrics-cli query 'hadoop_hbase_memheapusedm{cluster="mrs-hbase-oline", role="regionserver"}' --since 24h --step 15m

# Absolute past window in local time — for an alarm that already fired.
# --end pins the window end; --tz reads --end in that zone AND adds a time_local column.
# Alarm fired Beijing 11:57 → inspect that exact 6-minute window, rows tagged in Beijing time:
hbase-metrics-cli query 'hadoop_hbase_processcalltime_99_9th_percentile{cluster="mrs-hbase-oline-ng", instance="10.57.0.173:19110", sub="IPC"}' \
  --tz Asia/Shanghai --end "2026-07-01 12:00:00" --since 6m --step 30s --format table
```

`query` is **instant by default**; `--since` (or `--raw`, or `--end`) turns it into a range
query over `/api/v1/query_range`. Use the range form whenever you need to locate
*when* something peaked — the summary scenarios only give max/avg/p99/last, not
the timestamp. `--step` defaults to `auto`; `--raw` without `--since` uses a 5m
window. **`--end` + `--tz` remove the manual timezone math** this fleet's Beijing-time
alarms otherwise force: `--end` accepts unix seconds, RFC3339, or a zone-less
`"2006-01-02 15:04:05"` (read in `--tz`), and non-UTC `--tz` adds a `time_local`
column alongside the UTC `time`. Bad `--since`/`--step`/`--end`/`--tz` return
`FLAG_INVALID` (exit 2).

## Metric gotchas (silent no-data traps)

A wrong label filter returns **empty**, not an error — identical to "metric absent". When a `query` comes back empty, run `labels <metric>` before assuming the bean is missing.

- **`sub` disambiguates same-named metrics.** WAL fsync latency is `hadoop_hbase_synctime_99th_percentile{sub="WAL"}` (write path); RPC call time is `hadoop_hbase_processcalltime_99_9th_percentile{sub="IPC"}`. Mixing up the `sub` silently no-ops. This is the trap when confirming "write-driven".
- **Read/write rate is on gauges, not the counters you'd guess.** Use `read-write-split` (or `hadoop_hbase_readrequestratepersecond` / `writerequestratepersecond`, `sub="Server"`). `rpcmutaterequestcount` reads 0 on this fleet — trust `writerequestratepersecond` for writes.
- **Per-table × per-RS is NOT queryable from VM.** `metatable_table_<t>_request_*` only exists on the RS holding the `meta` region (0 / single-instance elsewhere), and per-table beans are blacklisted upstream. To judge one table's balance across RS, use balancer cost functions: `hadoop_hbase_<table>_tableskewcostfunction` / `_regioncountskewcostfunction` / `_readrequestcostfunction` / `_writerequestcostfunction` / `_storefilecostfunction` (`0` = balanced, non-zero = skew on that dimension).

## Common errors
| Code | Action |
|---|---|
| `CONFIG_MISSING` | run `hbase-metrics-cli config init` |
| `VM_UNREACHABLE` | check VPN / DNS to `vm_url`, raise `--timeout` |
| `VM_HTTP_4XX` 401/403 | set `HBASE_VM_USER` / `HBASE_VM_PASS` |
| `NO_DATA` | confirm `--cluster` matches an active label, widen `--since` |
| `FLAG_INVALID` (`--since` rejected) | the scenario is `instant` mode (see table above) — drop the flag |
