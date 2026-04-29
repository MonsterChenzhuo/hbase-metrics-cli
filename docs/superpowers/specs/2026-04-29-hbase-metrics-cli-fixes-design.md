# hbase-metrics-cli — v0.2.0 Fixes Design

- **Date**: 2026-04-29
- **Status**: Draft (awaiting user review)
- **Predecessor**: `2026-04-28-hbase-metrics-cli-design.md` (v0.1 baseline)
- **Target version**: `v0.2.0` (BREAKING)
- **Primary consumer**: Claude Code (and other AI agents)

## 摘要(中文)

v0.1 版本上线后,在真实 24h 诊断场景里暴露了 10 类问题。本设计在不引入第二份配置/第二个 HTTP 客户端/第二个错误体系的前提下,通过**新增聚合层**、**统一 `--since` 语义**、**修复 PromQL 抗毛刺**与**字段命名整理**,把 CLI 的默认输出从"对人不友好、对 agent 不可用"改造为"对 agent 直接可用、对人也更紧凑"。本变更标记为 BREAKING,通过新版本 v0.2.0 + CHANGELOG 迁移说明发布,旧行为保留在 `--raw` 标志下。

## 1. Goal

Make the default output of `hbase-metrics-cli` directly consumable by an AI agent — without any post-processing — for the most common analyst question: *"how is the cluster doing over the last N hours?"*

Concretely:

- A single `cluster-overview --since 24h` should produce ≤ 5 KB of JSON that already contains max / avg / p99 / last for QPS, RPC latency, GC, and heap.
- All time-range scenarios (`requests-qps`, `rpc-latency`, `gc-pressure`, `compaction-status`, `wal-stats`) shrink from ~1 MB raw datapoints to ~few KB of summary, while remaining drillable via `--raw`.
- PromQL templates no longer let counter resets fabricate phantom peaks (e.g. `write_qps_max ≈ 13 k/s` on a 70 qps cluster).
- Field names across scenarios are consistent (`regionservers_active`, `regions_total`, …) so agents do not have to learn two vocabularies.

## 2. Non-Goals

- No new transports, no second config format, no second exit-code scheme.
- No multi-cluster fan-out in a single command (still one cluster per invocation).
- No per-region / per-table metrics (still RS-granularity only — same blacklist).
- No live "watch" mode / no streaming output.
- No dashboard / web UI.

## 3. Problems Inventory

Each row maps a real symptom observed during the 2026-04-28 24h analysis to a concrete fix in this spec.

| # | Symptom (observed) | Root cause | Fix (section) |
|---|---|---|---|
| P1 | `cluster-overview` rejects `--since 24h` (`unknown flag`) | Instant scenario does not register `--since` | §4.1, §5.4 |
| P2 | Time-range scenarios output 0.97–1.6 MB JSON for 24h windows | `runner.go` writes raw datapoints arrays into rows | §5.1, §5.2 |
| P3 | `cluster-overview.regionserver_count = 2` while 3 RS are online | `count(...)` over Master metric is sensitive to label-set duplication | §5.5 (PromQL fix) |
| P4 | `requests-qps.write_qps_max = 13 129/s` on a ~70 qps cluster | `rate(counter[1m])` spikes on counter resets (region move, RS restart) | §5.3 |
| P5 | `blockcache-hitrate` returns `0%` on RS with zero reads — looks alarming | Ratio without volume context | §5.5 |
| P6 | Field names diverge: `regionserver_count` vs `regionservers_active` vs `regions` | No naming convention enforced | §5.5, §6 |
| P7 | `--step` is fixed at `30s`; 24h windows produce 2880 points × N series | No auto-resolution policy | §5.4 |
| P8 | `wal-stats` does not advertise the worst-instance summary | All work pushed to consumer (`jq`) | §5.2 (cross-instance roll-up) |
| P9 | Error message `unknown flag: --since` is opaque for instant scenarios | Cobra default; no scenario-aware hint | §5.4 |
| P10 | `SKILL.md` & `README` say `cluster-overview --since 30m` is valid (it is not) | Docs out of sync with code | §7 |

## 4. Command Surface Changes

### 4.1 New / changed global flags

| flag | scope | default | notes |
|---|---|---|---|
| `--raw` | all scenarios | `false` | When set, time-range scenarios emit raw datapoints (the v0.1 default). Instant scenarios ignore it. |
| `--since` | **all** scenarios (was: range-only) | `5m` (range) / unset (instant) | Instant scenarios accept it for "summarize over a window" mode (§5.4); when unset they keep instant behavior. |
| `--step` | range scenarios | `auto` (was: `30s`) | Resolved by `stepauto.Resolve(since)`; user-supplied value wins. |

No new global flags beyond these three. No removal.

### 4.2 Scenario-level changes

| scenario | change |
|---|---|
| `cluster-overview` | Becomes a *hybrid* scenario (§5.4): instant when `--since` absent, range-summary when present. |
| `requests-qps` | Default mode is `summary`; PromQL adjusted for counter-reset robustness (§5.3). |
| `rpc-latency` | Default mode is `summary`; columns: `p99_max`, `p99_p99_over_time`, `p99_avg`, `p99_last`, `p999_max`. |
| `gc-pressure` | Default mode is `summary`; `gc_time_max_ms_per_min`, `gc_time_avg_ms_per_min`, `gc_count_max_per_min`. |
| `compaction-status` | Default mode is `summary`; PromQL kept minimal; reports `queue_max`, `compacted_cells_qps_max`. |
| `wal-stats` | Default mode is `summary`; reports `append_qps_max/avg`, `sync_p99_max_ms`, `slow_appends_total`. |
| `blockcache-hitrate` | Adds `read_qps_recent` per row (volume context for hit-rate sanity). |
| `master-status` | Field rename (§6) for consistency with `cluster-overview`. |
| `regionserver-list` | Field rename (§6); no functional change. |
| `handler-queue` | No functional change; participates in `--since` rejection hint (§5.4). |
| `hotspot-detect` | No functional change. |
| `jvm-memory` | No functional change. |

## 5. Detailed Design

### 5.1 New package: `internal/aggregate`

Single-purpose module that turns a slice of `[ts, value-string]` datapoints (the VictoriaMetrics `query_range` shape, already exposed by `vmclient.Sample.Values`) into a fixed `Summary` struct.

```go
package aggregate

type Summary struct {
    Count     int     `json:"count"`              // number of valid samples
    Min       float64 `json:"min"`
    Max       float64 `json:"max"`
    Avg       float64 `json:"avg"`
    P50       float64 `json:"p50"`
    P95       float64 `json:"p95"`
    P99       float64 `json:"p99"`
    Last      float64 `json:"last"`
    NaNRatio  float64 `json:"nan_ratio,omitempty"` // share of samples that were NaN/+Inf
}

// Summarize returns Summary{Count:0} on empty input. NaN / +Inf samples are
// excluded from the order-statistics and Avg, but counted in NaNRatio so the
// caller can flag suspicious series.
func Summarize(values [][2]any) Summary
```

Boundary contract: `aggregate` knows nothing about scenarios, PromQL, or output formatting. It only reads the VM datapoint shape (`[unix_seconds_float, "value-string"]`) and emits scalars. This preserves the project's strict module boundaries (CLAUDE.md §Architecture).

### 5.2 Runner output mode routing

`cmd/scenarios/runner.go` gains a single decision point after `errgroup.Wait()`:

```
isRange := in.Scenario.Range || in.Since > 0       // §5.4 hybrid
useRaw  := in.Raw

switch {
case isRange && !useRaw:
    env.Mode = "summary"
    env.Data = summarizeByInstance(rendered, results, scenario)
    env.Columns = scenario.SummaryColumns()
case isRange && useRaw:
    env.Mode = "raw"
    env.Data = mergeByInstance(rendered, results, /*isRange=*/true)  // current path
    env.Columns = scenario.Columns
default:
    env.Mode = "instant"
    env.Data = mergeByInstance(rendered, results, /*isRange=*/false) // current path
}
```

`summarizeByInstance` calls `aggregate.Summarize` once per `(instance, query.label)` and flattens the resulting `Summary` into `<label>_max`, `<label>_avg`, `<label>_p99`, `<label>_last` columns.

Per-scenario customization happens through a new `summary:` block in YAML (§5.6) — scenarios that only care about `max + avg` (e.g. `gc-pressure.gc_time_ms_per_min`) declare so and the runner emits only those columns.

### 5.3 PromQL counter-reset hardening

PromQL changes are local to YAML; no Go change.

| query | v0.1 expression | v0.2 expression | rationale |
|---|---|---|---|
| `requests-qps.read_qps` | `rate(...readrequestcount[1m])` | `clamp_min(rate(...readrequestcount[5m]), 0)` | 5m window smooths reset spikes; `clamp_min` drops negative artifacts. |
| `requests-qps.write_qps` | `rate(...writerequestcount[1m])` | `clamp_min(rate(...writerequestcount[5m]), 0)` | same |
| `requests-qps.total_qps` | `rate(...totalrequestcount[1m])` | `clamp_min(rate(...totalrequestcount[5m]), 0)` | same |
| `cluster-overview.regionserver_count` | `count(numregionservers{role=master})` | `max(numregionservers{role=master, sub=Server})` | `count` over a Master metric was returning 2; the metric is already a scalar gauge — `max` is the correct reduction. |

The `summary` mode further mitigates by reporting `p99` alongside `max` so a single reset spike does not dominate the headline number.

### 5.4 Hybrid scenarios + auto-step + friendly error

Three coupled changes:

**(a) `--since` registration**

`register.go` registers `--since` and `--step` on **every** scenario (currently only on range scenarios). Behavior:

- Scenario.Range == true: as today.
- Scenario.Range == false **and** `--since` *unset*: behaves as today (instant query).
- Scenario.Range == false **and** `--since` *set*: scenario is treated as range-summary if it declares `instant_summary: true` in YAML (currently only `cluster-overview` does); otherwise the scenario emits a structured error with hint.

**(b) `--step auto`**

```go
// internal/stepauto/stepauto.go
func Resolve(since time.Duration) time.Duration {
    switch {
    case since <= 30*time.Minute: return 30 * time.Second
    case since <=  2*time.Hour:   return  1 * time.Minute
    case since <= 12*time.Hour:   return  2 * time.Minute
    case since <= 24*time.Hour:   return  5 * time.Minute
    default:                      return 10 * time.Minute
    }
}
```

User-supplied `--step <duration>` always wins. `--step auto` is the new default. Empty / unset stays auto for backward parsing.

**(c) Friendly rejection**

Instant scenarios that don't declare `instant_summary: true`, when given `--since`, return:

```
{"error":{
  "code":"FLAG_INVALID",
  "message":"scenario \"handler-queue\" is instant; --since does not apply",
  "hint":"omit --since for instant scenarios; or use a time-range scenario like rpc-latency / gc-pressure"
}}
```

Exit code stays `2` (user error). No new error code.

### 5.5 Per-scenario PromQL & schema fixes

#### cluster-overview (hybrid)

```yaml
name: cluster-overview
description: ...
range: false
instant_summary: true            # NEW: accepts --since for windowed summary
columns: [label, value]                                # used when --since absent (instant mode)
summary_columns: [label, max, avg, p99, last]          # used when --since set (summary mode)

queries:
  - label: regionservers_active
    expr: max(hadoop_hbase_numregionservers{cluster="{{.cluster}}", role="master", sub="Server"})
  - label: regionservers_dead
    expr: max(hadoop_hbase_numdeadregionservers{cluster="{{.cluster}}", role="master", sub="Server"})
  - label: regions_total
    expr: sum(hadoop_hbase_regioncount{cluster="{{.cluster}}", role="regionserver", sub="Server"})
  - label: qps_total
    expr: sum(clamp_min(rate(hadoop_hbase_totalrequestcount{cluster="{{.cluster}}", role="regionserver", sub="Server"}[5m]), 0))
  - label: rpc_p99_max_ms
    expr: max(hadoop_hbase_processcalltime_99th_percentile{cluster="{{.cluster}}", role="regionserver", sub="IPC"})
```

In `--since` mode, each row is `{label: <query_label>, max: <float>, avg: <float>, p99: <float>, last: <float>}`. There is no nested object — the summary aggs become top-level columns, matching the per-instance scenarios so consumers parse one shape.

Example (summary mode):

```json
{"label":"qps_total","max":70.93,"avg":2.55,"p99":12.4,"last":2.55}
```

Example (instant mode, unchanged):

```json
{"label":"qps_total","value":2.55}
```

#### blockcache-hitrate

Add `read_qps_recent` for volume context. `mode=summary` reports max/avg of hit ratio over the window.

#### master-status

Rename `regionservers_active`/`regionservers_dead` are kept (they were already correct here); other scenarios align to this canonical naming.

### 5.6 YAML schema additions

```yaml
# new optional fields on Scenario
instant_summary: false           # if true, instant scenario accepts --since (cluster-overview only for now)
summary_columns: []              # ordered list for --format table/markdown in summary mode
                                 # if absent, runner derives from queries: <label>_max, <label>_avg, <label>_p99, <label>_last
summary:                         # per-query aggregation overrides
  <query_label>:
    aggs: [max, avg, p99, last] # subset of [count, min, max, avg, p50, p95, p99, last]
```

Defaults: every range query reports `[max, avg, p99, last]` unless overridden.

`promql.Scenario` struct grows three fields (`InstantSummary bool`, `SummaryColumns []string`, `Summary map[string]SummarySpec`). `ParseScenario` validates that summary aggs use only the known keys.

### 5.7 Envelope schema delta

```diff
 type Envelope struct {
   Scenario string   `json:"scenario"`
   Cluster  string   `json:"cluster"`
+  Mode     string   `json:"mode"`           // "instant" | "summary" | "raw"
   Range    *Range   `json:"range,omitempty"`
   Queries  []Query  `json:"queries"`
   Columns  []string `json:"columns,omitempty"`
   Data     []Row    `json:"data"`
 }
```

`Mode` is **always populated** so consumers (agents, jq pipelines) can branch deterministically.

### 5.8 Field naming convention (across all scenarios)

Rules:

1. Snake-case, lowercase.
2. Counts use the plural noun (`regions_total`, `regionservers_active`).
3. Ratios use `_pct` suffix (existing `hit_ratio_pct`, kept).
4. Time values carry unit suffix (`_ms`, `_s`, `_min`).
5. Per-time-window rates carry `_per_min` / `_per_sec` suffix.
6. Summary suffixes are `_max`, `_avg`, `_p99`, `_last` (plus `_min`, `_p50`, `_p95` if requested).

Renames (BREAKING, listed in CHANGELOG):

| v0.1 | v0.2 |
|---|---|
| `cluster-overview.regionserver_count` | `cluster-overview.regionservers_active` |
| `cluster-overview.dead_regionserver_count` | `cluster-overview.regionservers_dead` |
| `cluster-overview.total_regions` | `cluster-overview.regions_total` |
| `cluster-overview.total_request_qps` | `cluster-overview.qps_total` |
| `cluster-overview.rpc_processcalltime_p99_max` | `cluster-overview.rpc_p99_max_ms` |
| `regionserver-list.qps` | `regionserver-list.qps_total` |
| `regionserver-list.regions` | `regionserver-list.regions_count` |

## 6. Migration

### From v0.1 to v0.2

| If you previously used … | Do this in v0.2 |
|---|---|
| `requests-qps --since 24h` and read raw points | Add `--raw` to keep the v0.1 shape; the default is now summary. |
| `cluster-overview` (instant) | No change. |
| `cluster-overview` and wanted 24h aggregates | Now: `cluster-overview --since 24h`. |
| Field `total_regions` in `cluster-overview` | Rename to `regions_total`. |
| Field `qps` in `regionserver-list` | Rename to `qps_total`. |

CHANGELOG.md gets a top section "v0.2.0 — BREAKING" with the full rename map and the `--raw` instructions.

## 7. Documentation Updates

| File | Change |
|---|---|
| `README.md` / `README.zh.md` | Add a new "Real-world example" section linking to `docs/examples/2026-04-29-24h-cluster-analysis.md`; document `--summary` (default) / `--raw`; show the new envelope schema; correct the scenarios table (`cluster-overview` accepts `--since`). |
| `CLAUDE.md` | Update Envelope schema; add summary aggregation contract; revise "Adding a new scenario" with the new `summary:` and `instant_summary:` fields; update "exit codes" section is unchanged. |
| `.claude/skills/hbase-metrics/SKILL.md` (in repo, copied to user `~/.claude/skills/...` by `install.sh`) | Update the playbook: every scenario can take `--since`; new "default summary" behavior; add an example with `cluster-overview --since 24h`. |
| `docs/examples/2026-04-29-24h-cluster-analysis.md` | New file. Verbatim 24h analysis report from 2026-04-29 conversation, retitled and wrapped with a short "how this was produced" preamble (one `cluster-overview --since 24h` invocation in v0.2). |
| `CHANGELOG.md` | New top section v0.2.0 with BREAKING marker and the rename / migration table. |

## 8. Testing Strategy

### 8.1 Unit (existing patterns)

- `internal/aggregate/summary_test.go` (new) — covers:
  - empty input, single point, NaN, +Inf
  - p99 / p95 boundary correctness vs known fixture
  - reset-spike fixture: max=13129, p99 < 50, avg < 250 (the live-cluster case)
- `internal/stepauto/stepauto_test.go` (new) — boundary tests at 30m / 2h / 12h / 24h.
- `internal/promql/scenarios_test.go` — extend to validate the new schema fields parse correctly.

### 8.2 Golden (extended)

Each range scenario gets two golden files:

```
tests/golden/<scenario>.golden.promql       (PromQL render — unchanged path)
tests/golden/<scenario>.summary.golden.json (envelope shape under summary mode)
tests/golden/<scenario>.raw.golden.json     (envelope shape under --raw)
```

Updated by `go test ./tests/golden -args -update`. The JSON goldens use a fixed fixture from a recorded VM response so they are deterministic.

### 8.3 e2e dry-run

`tests/e2e/dryrun_test.go` extended:

- `cluster-overview --since 24h --dry-run` is added to `allScenarios`.
- Verifies all 12 + the hybrid invocation succeed dry-run with `mode` populated.

### 8.4 Manual smoke (post-merge)

Operator runs against `https://vm.rupiahcepatweb.com/`:

```bash
hbase-metrics-cli cluster-overview --since 24h --format json | jq
hbase-metrics-cli rpc-latency --since 24h --format markdown
hbase-metrics-cli rpc-latency --since 24h --raw | jq '.data[0] | keys'
```

Expected: total stdout < 10 KB for the first two, raw mode produces the v0.1-shaped arrays.

## 9. Release & Rollout

- Branch: `release/v0.2.0`.
- Versioning: `v0.2.0` (BREAKING; pre-1.0 minor bump is sufficient per semver).
- Goreleaser config unchanged (the existing `dist/` 4-platform tarballs flow keeps working).
- `scripts/install.sh` re-tested; no flag changes.
- Skill bundle (`.claude/skills/hbase-metrics/SKILL.md`) shipped in the same release (CLAUDE.md §"Claude Code skill" rule already enforces this).

## 10. Out-of-scope follow-ups (tracked, not in this release)

- A `--profile` flag for multiple VM endpoints (would touch config; deferred).
- Per-table / per-region drill-down (still blocked by jmx_hbase.yaml blacklist; needs scrape-side work first).
- Live-tail mode (`watch -n 5 hbase-metrics-cli ...`).
- `query` escape hatch gaining its own `--summary` flag (the user can already pipe to `jq`).

## 11. Risks

| Risk | Mitigation |
|---|---|
| `aggregate.Summarize` math differs from VM's own quantiles | The summary `p99` is "p99 of the per-step values returned by VM over `--since`", not "p99 over raw observations". Field name stays `p99` (not `p99_over_time`) for brevity; this caveat is documented in `CLAUDE.md` under "Envelope schema" and shown once in `README` migration notes. |
| Some downstream consumer (a script, Grafana panel) parses the v0.1 raw shape | `--raw` keeps the v0.1 shape verbatim; CHANGELOG calls this out. |
| Counter-reset hardening (5m window) increases query load on VM | 5m window is still cheap; existing `errgroup.SetLimit(4)` cap protects VM. |
| Field rename breaks the example doc copy-pasted by users | Rename map is in CHANGELOG and README migration table. |
| `instant_summary: true` only on `cluster-overview` looks asymmetric | Acceptable v0.2 scope; we deliberately do *not* let `handler-queue --since` etc. silently summarize meaningless stateful gauges. Future scenarios opt in via the same flag. |

## 12. Acceptance Criteria

- [ ] `cluster-overview --since 24h` succeeds on the production VM and the resulting JSON < 5 KB with `mode == "summary"`.
- [ ] `rpc-latency --since 24h` JSON < 10 KB; `--raw` recovers the v0.1-shape ≥ 800 KB.
- [ ] `requests-qps --since 24h` reports `write_qps_p99 < write_qps_max`, with the gap matching the live-cluster reset-spike fixture.
- [ ] `regionservers_active` is `3` (not `2`) in `cluster-overview` against the live cluster.
- [ ] All 12 scenarios + the hybrid `cluster-overview --since` pass `make e2e-dry`.
- [ ] `make tidy && make lint && make unit-test` green.
- [ ] CHANGELOG / README / CLAUDE.md / SKILL.md / examples all updated in the same commit set.

