# CLAUDE.md

Project-specific instructions for Claude Code (and other AI agents) working in this repo.

## Project at a glance

`hbase-metrics-cli` is a Go CLI that diagnoses HBase clusters by running predefined PromQL scenarios against a VictoriaMetrics endpoint. Built primarily for AI agents — default output is structured JSON (with `--format table|markdown` for humans).

- **Module:** `github.com/opay-bigdata/hbase-metrics-cli`
- **Go:** 1.23+ (developed against go1.26.2)
- **Entry point:** `main.go` → `cmd.Execute()`
- **13 flat top-level scenario commands** are registered automatically by walking the embedded `scenarios/*.yaml`.

## Architecture (1-minute tour)

```
main.go
  └─ cmd/root.go                 cobra root, global flags, LoadEffectiveConfig()
       ├─ cmd/version.go         version subcommand + root --version flag (shared versionString())
       ├─ cmd/query.go           raw PromQL escape hatch: instant by default; --since/--raw/--end switch to a range query emitting flattened raw datapoints (warns when no cluster filter; columns derived from result labels). --end + --tz pin an absolute past window in a local timezone
       ├─ cmd/timewindow.go      --end / --tz parsing helpers (parseLocation, parseEndTime) shared by query
       ├─ cmd/clusters.go        list cluster= label values served by the VM endpoint
       ├─ cmd/labels.go          label-key discovery for a metric
       ├─ cmd/labelcheck.go      verify a label is actually emitted on a metric
       ├─ cmd/configcmd/         config init / config show (show goes through LoadEffectiveConfig so --env / HBASE_ENV / --vm-url are honored)
       └─ cmd/scenarios/         auto-registers one cobra cmd per YAML
            ├─ register.go       walks promql.LoadEmbedded(), wires flags (incl. --since/--step/--raw)
            ├─ runner.go         pickMode → render → errgroup parallel queries (limit 4) → merge/summarize → output
            ├─ summarize.go      summary-mode aggregation (per-instance & label-value), deterministic via sort.Slice
            └─ export_test_support.go  exposes buildEnvelope as BuildEnvelopeForGolden for envelope goldens

internal/
  ├─ aggregate/ pure summary math (max/avg/p99/last, NaN/Inf exclusion, NaNRatio)
  ├─ config/    layered config (flag > env > file > default), Source tracking
  ├─ errors/    CodedError {Code, Message, Hint}, exit codes 0/1/2/3
  ├─ output/    Envelope rendering: json | table | markdown (mode ∈ instant|summary|raw)
  ├─ promql/    embed.FS YAML loader + text/template renderer (Range/InstantSummary/Summary/SummaryColumns)
  ├─ stepauto/  auto-step resolver: 30m→30s, 2h→1m, 12h→2m, 24h→5m, >24h→10m
  └─ vmclient/  VM /api/v1/query{,_range} client with HTTP→error mapping

scenarios/        13 *.yaml + embed.go (//go:embed all:*.yaml)
tests/golden/     PromQL goldens + envelope JSON goldens (summary/raw shape locks) + golden_test.go (-update)
tests/e2e/        dryrun_test.go behind //go:build e2e (incl. hybrid cluster-overview check)
```

**Boundaries are strict:** `vmclient` knows nothing about scenarios; `promql` knows nothing about HTTP; `output` knows nothing about PromQL; `aggregate` is pure math (no I/O, no envelopes). Don't cross these.

## Build, test, lint

All gates run from the project root:

```bash
make tidy            # go mod tidy — must produce no diff in go.mod/go.sum
make lint            # go vet + gofmt -l + golangci-lint v2
make unit-test       # go test -race -count=1 ./...
make e2e-dry         # builds binary + runs the //go:build e2e dry-run suite
make build           # produces ./hbase-metrics-cli with embedded version
make release-snapshot  # goreleaser snapshot — 4 platform tarballs into dist/
```

Before committing changes you MUST verify `make tidy && make lint && make unit-test` are green.

## Maintaining docs after a fix

Every behaviour-changing fix MUST update documentation in the same commit. This file (`CLAUDE.md`) and the agent skill (`.claude/skills/hbase-metrics/SKILL.md`) are the entry points future agents (and humans) read first — stale docs cause the same bug to be re-hit.

After any of the following, update both files (and re-check `docs/superpowers/specs/*` if the contract changed):

- Adding / removing / renaming a scenario, flag, or exit code
- Changing a scenario's mode (`range` ↔ `hybrid` ↔ `instant`)
- Changing the Envelope shape, summary aggregation defaults, or stderr error codes
- Fixing an undocumented agent footgun (e.g. a flag that silently no-ops or hard-errors)

Concretely: the SKILL.md scenario table has a `Mode` column — keep it in sync with each YAML's `range:` / `instant_summary:` flags. If you change a scenario's mode, the table is wrong until you edit it.

## Adding a new scenario

Zero Go code changes. Drop a YAML into `scenarios/`, regenerate goldens, commit.

1. Create `scenarios/<my-scenario>.yaml`. Schema (v0.2.0):

```yaml
name: my-scenario               # must match filename stem
description: Short one-liner.
range: false                    # true for query_range, false for instant
instant_summary: false          # optional: instant + accepts --since for windowed summary
defaults:                       # only when range: true
  since: 5m
  step: 30s
flags:                          # optional per-scenario flags
  - name: top
    type: int                   # string | int
    default: 10
    enum: [10, 20]              # optional
    help: Top-K rows
columns: [instance, p99]        # column order in instant mode; raw prepends timestamp/time
summary_columns: [label, max, avg, p99, last]  # optional: column order in summary mode (label-value scenarios)
summary:                        # optional: per-query agg overrides (default [max, avg, p99, last])
  p99:
    aggs: [max, avg, p99, last]
queries:
  - label: p99
    expr: |
      topk({{.top}}, hadoop_hbase_..._99th_percentile{cluster="{{.cluster}}", role="RegionServer"})
```

**Mode routing** (compiled in `cmd/scenarios/runner.go::pickMode`):

```
mode = raw      if --raw
       summary  if scenario.range || (scenario.instant_summary && --since)
       instant  otherwise
```

**`instant_summary: true` is the default for per-RS gauge scenarios.** Without it, `--since` hard-errors `FLAG_INVALID` on the scenario, which forces agents to special-case the scenario list. If your new scenario produces a per-instance gauge / queue depth / hit ratio that an operator might want to look back over a window (and that's almost always), set `instant_summary: true` and provide a `summary:` block with the right aggregations. Reserve `range: true` for scenarios where instant has no useful meaning (rate / latency histograms).

**`{{.mode}}` / `{{.is_summary}}` template vars** are auto-injected by `runner.Run` on every render. `mode` is `"instant" | "summary" | "raw"`; `is_summary` is `true` when mode is `summary` or `raw`. Use them when the same scenario needs structurally different PromQL between modes — the canonical example is `topk(K, ...)` filtering, which is correct in instant but corrupts per-instance time series in summary because each scrape reshuffles which instances are in the top-K. Pattern from `scenarios/hotspot-detect.yaml`:

```yaml
queries:
  - label: qps
    expr: |
      {{- if .is_summary }}
      sum by (instance) (clamp_min(rate(hadoop_hbase_totalrequestcount{...}[5m]), 0))
      {{- else }}
      topk({{.top}}, sum by (instance) (clamp_min(rate(hadoop_hbase_totalrequestcount{...}[5m]), 0)))
      {{- end }}
```

When neither mode nor `is_summary` is set (e.g. test render with `Render(s, vars)` directly), they default to `"instant"` / `false` — so scenarios that don't reference them keep working.

**Schema invariant (Columns contract — both directions):** `fillMissingColumns` runs in every mode (`instant`, `summary`, `raw`) and enforces `set(row.keys) == set(env.Columns)` for every row in `env.Data`. Missing keys get `nil`; extra keys are deleted. The watchdog test `TestEnvelopeSchemaInvariant_AllScenarios` (in `cmd/scenarios/summarize_test.go`) loads every YAML and exercises this in all three modes against synthetic per-instance data — so schema drift between `summary_columns` / per-query `summary.<label>.aggs` will fail CI before it reaches an agent.

**HA-safe master queries:** the master metric series exists on **every** master replica (active + standby). An unwrapped instant query returns multiple series and `aggregateLabelValue` only renders the first one — usually standby's `0`. Always wrap master-server / master-AssignmentManager queries with `max(...)` so the active master's value wins. `cluster-overview.yaml` and `master-status.yaml` follow this pattern.

**Counter-reset convention:** every `rate()` expression wraps with `clamp_min(rate(...[5m]), 0)` and uses `[5m]` minimum. Use `max(...)` (not `count(...)`) for "active resource" gauges so a scrape miss doesn't drop the value.

2. Regenerate the golden file (note: zsh expands `./tests/golden/...` weirdly, so use the full import path):

```bash
go test -run TestRender_Goldens github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
```

3. Add the scenario name to `tests/e2e/dryrun_test.go` `allScenarios` slice.

4. Run `make unit-test && make e2e-dry`.

5. Template variables auto-available: `{{.cluster}}` (always set), plus any `flags:` you declared. `text/template` runs with `Option("missingkey=error")` — undefined vars fail loudly.

## Conventions you must follow

### Errors

Always return `*cerrors.CodedError` from RunE:

```go
import cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"

return cerrors.WithHint(
    cerrors.Errorf(cerrors.CodeFlagInvalid, "role %q not allowed", role),
    "use --role master or --role regionserver",
)
```

Codes are defined in `internal/errors/errors.go`. **stderr** carries the JSON error envelope; **stdout** stays data-only so pipes don't corrupt. Don't `fmt.Println(err)` from RunE.

### Output

Build an `output.Envelope` and call `output.Render(globals.Format, env, cmd.OutOrStdout())`. Don't print directly. Don't print to `os.Stdout` from inside `RunE` — use `cmd.OutOrStdout()` so tests can capture.

The envelope always includes `mode` (one of `instant` | `summary` | `raw`). Per-instance summary rows are sorted by `instance` for deterministic output.

**Columns contract (all modes):** every row in `env.Data` is guaranteed to contain a key for every entry in `env.Columns` — `buildEnvelope` runs `fillMissingColumns` as a final pass and inserts explicit `nil` for any missing key. AI agents can rely on the column header as a stable schema. If you add a new output path, either keep this invariant or extend `fillMissingColumns`.

**Raw mode shape:** `--raw` flattens range datapoints into one row per timestamp instead of nesting Prometheus matrix arrays. Per-instance scenarios emit `columns: [instance, timestamp, time, <query labels...>]`; label-value scenarios emit `columns: [label, timestamp, time, value]`. `timestamp` is Unix seconds and `time` is UTC RFC3339. This is intentionally easier for agents to filter around an alarm minute with `jq` and easier for table/markdown rendering.

**Cloud HostName vs metric instance:** HUAWEI CLOUD alarms often report `HostName=node-...`, but the VM label set emitted by this exporter only includes `instance` (`IP:port`), not `hostname`. Use `labels <metric>` or `label-check <metric> hostname` to verify labels, then map `HostName` to `instance` via Manager/CMDB/inventory before attributing a host-specific alarm to an RS. Do not infer the mapping from row order.

**No-data signaling:** `pickAgg` returns `nil` (JSON `null`) for every numeric aggregate when the underlying `aggregate.Summary` has no valid datapoints (`Count == 0` or `NaNRatio >= 1.0`). This distinguishes "the metric had no samples in the window" from "the metric's last value was 0" — critical for low-traffic clusters where `last=0` would otherwise be ambiguous. `count` and `nan_ratio` aggs always stay numeric (they describe coverage, not the metric).

### HTTP

Always go through `vmclient.New(vmclient.Options{...}).Query{,Range}(ctx, ...)`. The client does HTTP→`CodedError` mapping (5xx → `CodeVMHTTP5XX`, 4xx → `CodeVMHTTP4XX`, transport → `CodeVMUnreachable`). Don't add a second HTTP client.

**Retry**: `vmclient` retries transient failures with exponential backoff (default 200ms × 3, capped by `MaxRetries` = 2 = three total attempts). Retried: network errors, HTTP 429, HTTP 5XX. Not retried: 4XX 401/403/404/422 (permanent — auth/parse). Tune via `Options.MaxRetries` / `Options.RetryBaseDelay`. Backoff respects `ctx`. Tests live in `internal/vmclient/vmclient_test.go::TestRetry_*`.

For schema discovery (label keys / values), use `vmclient.Series(ctx, selector, since)` — it hits `/api/v1/series` and reuses the same retry path.

### Concurrency

Parallel queries inside a scenario use `errgroup.WithContext` with `SetLimit(4)`. If you add new parallel work in `cmd/scenarios/runner.go`, keep the cap at 4 — VM rate limiting was the reason.

## Multi-env config (v0.2.x+)

A single `~/.config/hbase-metrics-cli/config.yaml` can hold multiple named environment profiles in an `envs:` map, with `active_env:` picking the default. This lets one binary point at NG / ID / staging without re-editing the file.

Schema:

```yaml
active_env: nigeria              # default profile; can be overridden at runtime
envs:
  nigeria:
    vm_url: http://ng.example.com/
    default_cluster: mrs-hbase-oline-ng
  indonesia:
    vm_url: https://id.example.com/
    default_cluster: mrs-hbase-oline
# Flat top-level fields remain valid; they act as fallback when no profile
# applies or when the chosen profile leaves a field empty.
vm_url: http://ng.example.com/
default_cluster: mrs-hbase-oline-ng
timeout: 10s
```

Profile-name precedence (low → high):

1. `cfg.ActiveEnv` (from YAML)
2. `HBASE_ENV` env var
3. `--env <name>` flag

Field-value precedence (low → high) inside `LoadEffectiveConfig` (`cmd/root.go`):

```
default → file flat → env profile overlay → HBASE_* env vars → --vm-url etc. flags
```

The overlay only touches non-empty profile fields; everything else falls through to the flat top-level. Unknown env names hard-error with `CONFIG_INVALID` and a hint listing known names.

`config show` always reflects the **resolved** profile via `selected_env` (vs. `active_env` which is the YAML default) and tags `vm_url` / `default_cluster` sources as `env_profile` when they came from the overlay.

**Don't reuse `--cluster` for profile selection** — it already means the PromQL `{cluster="..."}` label value (a metric dimension, not a config profile). The flag is `--env`; the YAML field is `active_env`; the env var is `HBASE_ENV`.

## Schema-discovery subcommands (v0.2.x)

When writing PromQL or debugging "filter returns nothing", reach for these
before guessing — broken label filters are silently empty in PromQL and
look identical to "no data".

```bash
# Which HBase clusters does this VM endpoint serve? (cluster= label values)
hbase-metrics-cli clusters

# What labels does this metric carry, and how many distinct values each?
hbase-metrics-cli labels hadoop_hbase_clusterrequests

# Does this metric actually expose a `master` label?
hbase-metrics-cli label-check hadoop_hbase_clusterrequests master
```

`clusters` lists the distinct `cluster=` label values across `hadoop_hbase_*`
series via `/api/v1/label/cluster/values` (`vmclient.LabelValues`); the row
matching `default_cluster` is flagged `default: true`. There is intentionally
no `list-clusters`-style config command — a cluster is a PromQL label value,
not a config entity. Feed the names to `--cluster`, or wire them into `envs:`
profiles for `--env` switching.

`labels` / `label-check` hit `/api/v1/series` (via `vmclient.Series`), auto-scope
to `--cluster` if set, and emit the standard envelope. `label-check` returns
`status: present|missing` with a hint pointing at alternatives when
missing — useful since PromQL won't error out on absent labels, it just
silently no-ops the filter.

The `query` subcommand emits a stderr warning when the raw PromQL has no
`cluster=` selector. Non-blocking — pipe through `2>/dev/null` if you
intentionally want a multi-cluster view.

**`query` instant vs range (v0.2.x).** `query` is instant by default
(`mode: "instant"`, one row per series). It becomes a **range** query when you
pass `--since` (Go duration like `30m`, `24h`) or the global `--raw` flag —
then it hits `/api/v1/query_range` and emits the same flattened raw shape as
scenarios: `mode: "raw"`, `columns: [instance, timestamp, time, value]`, one
row per (instance, timestamp), plus a `range` block. `--step` defaults to
`auto` (resolved via `internal/stepauto`); `--raw` without `--since` defaults
to a 5m window. This closes an AI footgun: previously `query --since` hard-errored
`unknown flag` and `query --raw` silently no-op'd back to instant with an empty
`mode`, so the escape hatch couldn't fetch the time series needed to locate a
peak minute. Bad `--since`/`--step` now return `FLAG_INVALID` (exit 2), not
`INTERNAL`.

**`query --end` / `--tz` (absolute past window in a local timezone).** `--since`
only ever looks back from *now*, so investigating an alarm that fired hours ago
meant hand-computing Unix timestamps and dropping to raw `curl /api/v1/query_range`.
`--end` pins the window's end at an absolute instant; combined with `--since` it
selects exactly `[end-since, end]`. `--end` accepts **unix seconds**, **RFC3339**
(with its own zone), or a **zone-less wall-clock** `"2006-01-02 15:04:05"` /
`"...T..."` / minute-precision form. `--tz` sets the timezone that zone-less
`--end` values are interpreted in, and adds a `time_local` column to the raw
output so rows line up with the alarm's local clock without manual `+08:00`
arithmetic. `--tz` takes an IANA name (`Asia/Shanghai`) or a fixed offset
(`+08:00`, `+0800`, `+8`, `-05:00`); default is UTC and the `time_local` column
is omitted (unchanged contract). Bad `--tz`/`--end` return `FLAG_INVALID`
(exit 2). Alarms in this fleet report **Beijing time (UTC+8)** while VM stores
UTC — the canonical drill-in is:

```bash
# Alarm fired Beijing 11:57 → look at that 6-minute window, rows tagged in Beijing time
hbase-metrics-cli query 'hadoop_hbase_processcalltime_99_9th_percentile{cluster="mrs-hbase-oline-ng", instance="10.57.0.173:19110", sub="IPC"}' \
  --tz Asia/Shanghai --end "2026-07-01 12:00:00" --since 6m --step 30s --format table
```

## Things NOT to do

- **Don't put scenario YAMLs in subdirectories** — `//go:embed all:*.yaml` is intentionally non-recursive in this layout, and `_meta.yaml` is a placeholder filtered by the `_` prefix.
- **Don't add per-region or per-table HBase metrics** — those JMX beans are blacklisted in the upstream `jmx_hbase.yaml` scrape config (cardinality control). Hotspot detection lives at RegionServer granularity only.
- **Don't introduce a second config format / second exit-code scheme / second logger.** One of each, already wired.
- **Don't change exit codes.** They're part of the agent contract: `0` success or NoData warning · `1` internal · `2` user error · `3` VM failure.
- **Don't gofmt-skip.** `make lint` enforces gofmt + goimports via golangci-lint v2's `formatters` block.
- **Don't `--no-verify` a commit.** Hooks aren't currently configured but if they get added, fix the issue rather than skip.
- **Don't ship a behaviour fix without updating CLAUDE.md and SKILL.md.** See "Maintaining docs after a fix" above. The two files are the agent contract; if they disagree with the code, the code is what runs but the next agent will read the doc first and waste an iteration.

## Useful local URLs / labels

- Production VM: `https://vm.rupiahcepatweb.com/`
- Default cluster label: `mrs-hbase-oline`
- Cluster human name: `mrs印尼集群`
- Label set on every series: `platform`, `cluster`, `cluster_name`, `service`, `role`, `instance`
- Metric prefixes: `hadoop_hbase_*` (HBase JMX), `jvm_*` (JVM, GC, Threading, OS)

## Spec & plan

- v0.1 design spec: `docs/superpowers/specs/2026-04-28-hbase-metrics-cli-design.md`
- v0.1 implementation plan: `docs/superpowers/plans/2026-04-28-hbase-metrics-cli.md`
- v0.2 fixes spec: `docs/superpowers/specs/2026-04-29-hbase-metrics-cli-fixes-design.md`
- v0.2 fixes plan: `docs/superpowers/plans/2026-04-29-hbase-metrics-cli-v020-fixes.md`
- Release notes: `CHANGELOG.md` (see v0.2.0 for the rename map and migration steps)
- Live walk-through: `docs/examples/2026-04-29-24h-cluster-analysis.md`

The specs are authoritative when in doubt about behavior — read those before changing exit codes, the Envelope schema, or the agent contract.

## Claude Code skill

The `.claude/skills/hbase-metrics/SKILL.md` is the agent-facing entry point. If you change the scenario list, command names, exit codes, Envelope schema, or any scenario's `Mode` (`range` / `hybrid` / `instant`), **update the skill in the same commit** so agents using it don't drift.
