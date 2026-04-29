# Changelog

## v0.2.0 — 2026-04-29

### Highlights
- Hybrid mode for instant scenarios: `--since` is now accepted by every scenario. By default, instant scenarios under `--since` return a **summary** (per-instance/per-label aggregates of count/min/max/avg/p50/p95/p99/last) instead of raw datapoints. Pass `--raw` to opt into the original raw point matrix.
- Auto-resolved `--step`: when `--since` is set without `--step`, the CLI picks 30s/1m/2m/5m/10m based on window size.
- Counter-reset hardening: every `rate()` query now uses `clamp_min(rate(...[5m]), 0)` to suppress phantom spikes after restarts.
- Stable schema with the new `mode` envelope field (`instant` | `summary` | `raw`).
- Better error: passing `--since` to a non-hybrid scenario fails with `FLAG_INVALID` and a clear hint.

### Field rename map (BREAKING)

| Scenario | Before | After |
|---|---|---|
| cluster-overview | `regionserver_count` | `regionservers_active` |
| cluster-overview | `dead_regionserver_count` | `regionservers_dead` |
| cluster-overview | `total_regions` | `regions_total` |
| cluster-overview | `total_request_qps` | `qps_total` |
| cluster-overview | `rpc_processcalltime_p99_max` | `rpc_p99_max_ms` |
| regionserver-list | `qps` | `qps_total` |
| regionserver-list | `regions` | `regions_count` |

Downstream agents / dashboards that consumed those JSON keys must update.

### Aggregation semantics
- `regionservers_active` now uses `max(...)` instead of `count(...)` — fixes the case where a scrape miss made the count drop momentarily.
- `qps_total` and `read/write_qps` use `clamp_min(rate(...[5m]), 0)` — fixes phantom 13K/s spikes from counter resets.
- `blockcache-hitrate` adds `read_qps_recent` so the agent can interpret hit-rate=0% at zero reads correctly.

### Output envelope
- Added `mode` field, always one of `instant`, `summary`, `raw`.
- Summary envelope columns: per-instance shape uses `<label>_<agg>` headers; per-label-value shape uses flat `[label, max, avg, p99, last]`.
- Per-instance rows are now sorted by `instance` for deterministic output.

### Migration
1. Update consumers of the renamed fields (table above).
2. If you previously parsed time-series payloads, switch to the new summary mode (default) or pass `--raw` for the legacy shape.
3. Adjust scripts that hard-coded `--step=30s`; let auto-step pick.
