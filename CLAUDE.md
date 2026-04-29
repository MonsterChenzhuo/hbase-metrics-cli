# CLAUDE.md

Project-specific instructions for Claude Code (and other AI agents) working in this repo.

## Project at a glance

`hbase-metrics-cli` is a Go CLI that diagnoses HBase clusters by running predefined PromQL scenarios against a VictoriaMetrics endpoint. Built primarily for AI agents — default output is structured JSON (with `--format table|markdown` for humans).

- **Module:** `github.com/opay-bigdata/hbase-metrics-cli`
- **Go:** 1.23+ (developed against go1.26.2)
- **Entry point:** `main.go` → `cmd.Execute()`
- **12 flat top-level scenario commands** are registered automatically by walking the embedded `scenarios/*.yaml`.

## Architecture (1-minute tour)

```
main.go
  └─ cmd/root.go                 cobra root, global flags, LoadEffectiveConfig()
       ├─ cmd/version.go         version subcommand
       ├─ cmd/query.go           raw PromQL escape hatch (warns when no cluster filter)
       ├─ cmd/labels.go          label-key discovery for a metric
       ├─ cmd/labelcheck.go      verify a label is actually emitted on a metric
       ├─ cmd/configcmd/         config init / config show
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

scenarios/        12 *.yaml + embed.go (//go:embed all:*.yaml)
tests/golden/     12 PromQL goldens + 10 envelope JSON goldens (summary/raw shape locks) + golden_test.go (-update)
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
columns: [instance, p99]        # column order in raw / instant mode
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

### HTTP

Always go through `vmclient.New(vmclient.Options{...}).Query{,Range}(ctx, ...)`. The client does HTTP→`CodedError` mapping (5xx → `CodeVMHTTP5XX`, 4xx → `CodeVMHTTP4XX`, transport → `CodeVMUnreachable`). Don't add a second HTTP client.

**Retry**: `vmclient` retries transient failures with exponential backoff (default 200ms × 3, capped by `MaxRetries` = 2 = three total attempts). Retried: network errors, HTTP 429, HTTP 5XX. Not retried: 4XX 401/403/404/422 (permanent — auth/parse). Tune via `Options.MaxRetries` / `Options.RetryBaseDelay`. Backoff respects `ctx`. Tests live in `internal/vmclient/vmclient_test.go::TestRetry_*`.

For schema discovery (label keys / values), use `vmclient.Series(ctx, selector, since)` — it hits `/api/v1/series` and reuses the same retry path.

### Concurrency

Parallel queries inside a scenario use `errgroup.WithContext` with `SetLimit(4)`. If you add new parallel work in `cmd/scenarios/runner.go`, keep the cap at 4 — VM rate limiting was the reason.

## Schema-discovery subcommands (v0.2.x)

When writing PromQL or debugging "filter returns nothing", reach for these
before guessing — broken label filters are silently empty in PromQL and
look identical to "no data".

```bash
# What labels does this metric carry, and how many distinct values each?
hbase-metrics-cli labels hadoop_hbase_clusterrequests

# Does this metric actually expose a `master` label?
hbase-metrics-cli label-check hadoop_hbase_clusterrequests master
```

Both hit `/api/v1/series` (via `vmclient.Series`), auto-scope to
`--cluster` if set, and emit the standard envelope. `label-check` returns
`status: present|missing` with a hint pointing at alternatives when
missing — useful since PromQL won't error out on absent labels, it just
silently no-ops the filter.

The `query` subcommand emits a stderr warning when the raw PromQL has no
`cluster=` selector. Non-blocking — pipe through `2>/dev/null` if you
intentionally want a multi-cluster view.

## Things NOT to do

- **Don't put scenario YAMLs in subdirectories** — `//go:embed all:*.yaml` is intentionally non-recursive in this layout, and `_meta.yaml` is a placeholder filtered by the `_` prefix.
- **Don't add per-region or per-table HBase metrics** — those JMX beans are blacklisted in the upstream `jmx_hbase.yaml` scrape config (cardinality control). Hotspot detection lives at RegionServer granularity only.
- **Don't introduce a second config format / second exit-code scheme / second logger.** One of each, already wired.
- **Don't change exit codes.** They're part of the agent contract: `0` success or NoData warning · `1` internal · `2` user error · `3` VM failure.
- **Don't gofmt-skip.** `make lint` enforces gofmt + goimports via golangci-lint v2's `formatters` block.
- **Don't `--no-verify` a commit.** Hooks aren't currently configured but if they get added, fix the issue rather than skip.

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

The `.claude/skills/hbase-metrics/SKILL.md` is the agent-facing entry point. If you change the scenario list, command names, exit codes, or Envelope schema, **update the skill in the same commit** so agents using it don't drift.
