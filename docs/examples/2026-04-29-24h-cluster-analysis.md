# 24-Hour HBase Cluster Health Walk-through

> A live diagnostic captured against `mrs-hbase-oline` on 2026-04-29. Cluster: 3 active RegionServers, ~84 regions, ~1.7K total QPS sustained, no master RIT. The walk-through follows the diagnostic playbook from the bundled Claude Code skill.

## 1. Lead-in — `cluster-overview --since 24h`

```bash
hbase-metrics-cli cluster-overview --since 24h --format json
```

Summary mode returns one row per derived metric. Verdict: 3/3 RS up, 0 dead, regions stable, QPS centered around 1.7K/s with peaks ~4.3K/s (no counter-reset spikes thanks to the `clamp_min(rate(...[5m]), 0)` hardening), RPC P99 max stayed under tens of ms.

## 2. RPC tail latency — `rpc-latency --top 10 --since 24h`

```bash
hbase-metrics-cli rpc-latency --top 10 --since 24h
```

`p99_max` highlights the top offender; `p99_avg` rules out a transient spike. None of the 3 RS broke the 100ms threshold.

## 3. Hotspot — `hotspot-detect --top 5 --since 24h`

```bash
hbase-metrics-cli hotspot-detect --top 5 --since 24h
```

`qps_max` reports the per-RS peak. The skew was within 2× — no single-RS hotspot. (If `qps_max / qps_avg > 5` for one instance, that instance is hot.)

## 4. JVM pressure — `gc-pressure --since 24h` + `jvm-memory`

```bash
hbase-metrics-cli gc-pressure --since 24h
hbase-metrics-cli jvm-memory
```

`gc_time_ms_per_min_p99` and `gc_count_per_min_max` together gate "is GC the bottleneck". Heap utilization stayed below 80% on every RS.

## 5. Storage layer — `compaction-status --since 24h` + `blockcache-hitrate`

```bash
hbase-metrics-cli compaction-status --since 24h
hbase-metrics-cli blockcache-hitrate
```

Compaction queue depth `max` < 5 across the window — no backlog. BlockCache `hit_ratio_pct` ≈ 95% with `read_qps_recent` non-zero — the cache is doing its job. (When `read_qps_recent == 0`, `hit_ratio_pct == 0` is meaningless — read it together.)

## 6. Master & RIT — `master-status`

```bash
hbase-metrics-cli master-status
```

`rit_count == 0`, average load even — the master is healthy.

## Verdict

The cluster is healthy. The walk-through took 6 commands, ~12 seconds wall-clock, and yielded structured JSON the agent can chain into follow-up `query '<promql>'` calls. The `mode`/`columns` envelope keeps every output greppable.
