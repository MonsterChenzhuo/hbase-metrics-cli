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
       ├─ cmd/query.go           raw PromQL escape hatch
       ├─ cmd/configcmd/         config init / config show
       └─ cmd/scenarios/         auto-registers one cobra cmd per YAML
            ├─ register.go       walks promql.LoadEmbedded(), wires flags
            └─ runner.go         render → errgroup parallel queries (limit 4) → merge by instance → output

internal/
  ├─ config/    layered config (flag > env > file > default), Source tracking
  ├─ errors/    CodedError {Code, Message, Hint}, exit codes 0/1/2/3
  ├─ output/    Envelope rendering: json | table | markdown
  ├─ promql/    embed.FS YAML loader + text/template renderer
  └─ vmclient/  VM /api/v1/query{,_range} client with HTTP→error mapping

scenarios/        12 *.yaml + embed.go (//go:embed all:*.yaml)
tests/golden/     12 golden PromQL files + golden_test.go (-update flag)
tests/e2e/        dryrun_test.go behind //go:build e2e
```

**Boundaries are strict:** `vmclient` knows nothing about scenarios; `promql` knows nothing about HTTP; `output` knows nothing about PromQL. Don't cross these.

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

### Concurrency

Parallel queries inside a scenario use `errgroup.WithContext` with `SetLimit(4)`. If you add new parallel work in `cmd/scenarios/runner.go`, keep the cap at 4 — VM rate limiting was the reason.

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

- Design spec: `docs/superpowers/specs/2026-04-28-hbase-metrics-cli-design.md`
- Implementation plan: `docs/superpowers/plans/2026-04-28-hbase-metrics-cli.md`

Both are authoritative when in doubt about behavior — read those before changing exit codes, the Envelope schema, or the agent contract.

## Claude Code skill

The `.claude/skills/hbase-metrics/SKILL.md` is the agent-facing entry point. If you change the scenario list, command names, exit codes, or Envelope schema, **update the skill in the same commit** so agents using it don't drift.
