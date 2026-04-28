# hbase-metrics-cli — Design Spec

- **Date**: 2026-04-28
- **Status**: Draft (awaiting user review)
- **Reference project**: `../cli/` (lark-cli) — agent-native CLI patterns
- **Primary consumer**: Claude Code (and other AI Agents)

## 1. Goal

Build a Go CLI that lets Claude Code diagnose HBase cluster issues by running named scenario commands against VictoriaMetrics. The CLI:

- Ships as a single static Go binary plus a Claude Code skill in `.claude/skills/hbase-metrics/`.
- Exposes 12 predefined diagnostic scenarios as flat top-level subcommands.
- Renders PromQL from embedded YAML templates with the user's cluster labels injected.
- Returns structured JSON by default (with `table` / `markdown` alternatives) and structured stderr errors with actionable hints.
- Supports a default cluster from config plus `--cluster` override; optional Basic Auth.

## 2. Non-Goals (v1)

- No npm distribution. No multi-profile switching à la lark-cli.
- No keychain credential storage. Basic Auth via env vars or config file plaintext is sufficient for the internal-network deployment.
- No self-update mechanism. Releases via GoReleaser binaries + `go install` are enough.
- No two-level subcommand grouping. Flat 12 scenarios.
- No per-region / per-table metrics — those JMX beans are blacklisted in the scrape config (`jmx_hbase.yaml`), so hotspot detection lives at RegionServer granularity only.

## 3. Deployment Context

### 3.1 Metrics pipeline

```
HBase Master/RegionServer
  └─ jmx_prometheus_exporter (per jmx_hbase.yaml rules)
       └─ Prometheus-compatible scrape (per indonesia-mrs-hbase-oline.yaml)
            └─ VictoriaMetrics  (https://vm.rupiahcepatweb.com/)
                 └─ hbase-metrics-cli  ←  Claude Code
```

### 3.2 Label & metric conventions (from user-provided configs)

Labels applied at scrape time:

| label | example values |
|---|---|
| `platform` | `mrs` |
| `cluster` | `mrs-hbase-oline` |
| `cluster_name` | `mrs印尼集群` |
| `service` | `hbase` |
| `role` | `master`, `regionserver` |
| `instance` | `10.94.0.156:19110` |

Metric naming (from `jmx_hbase.yaml`, `lowercaseOutputName: true`):

- HBase JMX: `hadoop_hbase_<metric>` with labels `role`, `sub` (e.g. `Server`, `IPC`, `JvmMetrics`).
- JVM: `jvm_memory_<usage>_<field>`, `jvm_gc_<field>` (label `gc`), `jvm_threading_<field>`, `jvm_os_<field>`.

The CLI's PromQL templates assume these names. Metric prefixes are configurable in templates (YAML) so future clusters with different exporters can be supported by editing template files only — no Go changes.

## 4. Command Surface

### 4.1 Flat scenario commands (12)

| command | purpose |
|---|---|
| `cluster-overview` | RS count / total regions / total request QPS / RPC P99 |
| `regionserver-list` | per-RS region count, qps, heap |
| `rpc-latency` | per-RS / Master RPC P50/P99/P999 |
| `handler-queue` | IPC handler queue length + busy ratio |
| `requests-qps` | read / write / total request QPS |
| `gc-pressure` | GC count and elapsed time |
| `jvm-memory` | heap / non-heap usage ratio |
| `compaction-status` | compaction queue length and elapsed time |
| `master-status` | Master state, balancer, in-transition regions |
| `wal-stats` | WAL write QPS / latency / sliding window |
| `blockcache-hitrate` | BlockCache hit rate / size |
| `hotspot-detect` | top-K hot RS by qps and handler busy ratio |

### 4.2 Utility commands

| command | purpose |
|---|---|
| `config init` | Interactive setup: writes `~/.config/hbase-metrics-cli/config.yaml` |
| `config show` | Print effective config (with sources: flag/env/file/default) |
| `query '<promql>'` | Escape hatch for raw PromQL (instant query) |
| `version` | Build info |

### 4.3 Global flags

| flag | env | default |
|---|---|---|
| `--vm-url` | `HBASE_VM_URL` | from config |
| `--cluster` | `HBASE_CLUSTER` | from config (`default_cluster`) |
| `--basic-auth-user` | `HBASE_VM_USER` | empty |
| `--basic-auth-pass` | `HBASE_VM_PASS` | empty |
| `--timeout` | `HBASE_VM_TIMEOUT` | `10s` |
| `--format` | — | `json` (alts: `table`, `markdown`) |
| `--dry-run` | — | `false` (prints rendered PromQL, skips HTTP) |

### 4.4 Common scenario flags

| flag | applies to | default |
|---|---|---|
| `--role` | scenarios that filter by master/regionserver | `regionserver` |
| `--instance` | most scenarios | unset (all) |
| `--since` | range queries | `5m` |
| `--step` | range queries | `30s` |
| `--top` | top-K scenarios | `10` |

## 5. Architecture

### 5.1 Repository layout

```
hbase-metrics-cli/
├── main.go
├── go.mod
├── Makefile
├── README.md                       # English
├── README.zh.md                    # 中文
├── .goreleaser.yml
├── .golangci.yml
├── .gitignore
├── .claude/
│   └── skills/
│       └── hbase-metrics/
│           └── SKILL.md
├── cmd/
│   ├── root.go
│   ├── version.go
│   ├── query.go
│   ├── config/
│   │   ├── init.go
│   │   └── show.go
│   └── scenarios/
│       ├── register.go             # walks embed.FS, registers cobra cmds
│       ├── runner.go               # shared exec pipeline
│       ├── cluster_overview.go     # one file per scenario, ~60 LOC each
│       ├── regionserver_list.go
│       ├── rpc_latency.go
│       ├── handler_queue.go
│       ├── requests_qps.go
│       ├── gc_pressure.go
│       ├── jvm_memory.go
│       ├── compaction_status.go
│       ├── master_status.go
│       ├── wal_stats.go
│       ├── blockcache_hitrate.go
│       └── hotspot_detect.go
├── internal/
│   ├── config/
│   ├── vmclient/
│   ├── promql/
│   ├── output/
│   └── errors/
├── scenarios/                      # embed.FS source
│   ├── cluster-overview.yaml
│   ├── regionserver-list.yaml
│   ├── rpc-latency.yaml
│   ├── handler-queue.yaml
│   ├── requests-qps.yaml
│   ├── gc-pressure.yaml
│   ├── jvm-memory.yaml
│   ├── compaction-status.yaml
│   ├── master-status.yaml
│   ├── wal-stats.yaml
│   ├── blockcache-hitrate.yaml
│   └── hotspot-detect.yaml
├── tests/
│   ├── unit/
│   ├── golden/                     # rendered PromQL goldens
│   └── e2e/
└── docs/
    └── superpowers/specs/2026-04-28-hbase-metrics-cli-design.md
```

### 5.2 Internal packages

| package | responsibility | public surface | dependencies |
|---|---|---|---|
| `internal/config` | Load/save YAML config, layered overrides (flag > env > file > default) | `Load() (*Config, error)`, `Save(*Config) error`, `Config{VMURL, DefaultCluster, BasicAuth, Timeout}` | stdlib + `gopkg.in/yaml.v3` |
| `internal/vmclient` | HTTP client for VM `/api/v1/query`, `/api/v1/query_range`, `/api/v1/labels` | `New(opts) *Client`, `Query(ctx, expr, ts)`, `QueryRange(ctx, expr, start, end, step)` | `net/http` |
| `internal/promql` | Load embedded scenario YAMLs, render PromQL with vars | `Load() ([]Scenario, error)`, `Render(name string, vars Vars) ([]RenderedQuery, error)` | `text/template`, `embed` |
| `internal/output` | Render results to json / table / markdown | `Render(format string, env Envelope, w io.Writer) error` | `text/tabwriter` |
| `internal/errors` | Structured stderr errors `{code, message, hint}` | `Errorf(code, format, args...) error`, `WithHint(err, hint) error` | stdlib |
| `cmd/scenarios/runner` | Shared pipeline: resolve config → render PromQL → execute (errgroup, max 4 concurrent) → render output | `Run(ctx, scenarioName, vars, opts) error` | all of the above |

**Boundaries**: vmclient knows nothing about scenarios; promql knows nothing about HTTP; output knows nothing about PromQL. Each is independently testable.

### 5.3 Scenario YAML format

```yaml
name: rpc-latency
description: HBase RegionServer/Master RPC P50/P99/P999 latency
range: true                         # true = query_range, false = instant query
defaults:
  since: 5m
  step: 30s
flags:
  - name: role
    default: regionserver
    enum: [master, regionserver]
  - name: top
    type: int
    default: 10
queries:
  - label: p99
    expr: |
      topk({{.top}}, hadoop_hbase_ipc_processcalltime_99th_percentile{
        cluster="{{.cluster}}", role="{{.role}}"
      })
  - label: p999
    expr: |
      topk({{.top}}, hadoop_hbase_ipc_processcalltime_999th_percentile{
        cluster="{{.cluster}}", role="{{.role}}"
      })
columns: [instance, p99, p999]
```

Adding a 13th scenario = drop one YAML + run `register.go`'s embed walk; no Go edits required (beyond the per-scenario `cmd/scenarios/<name>.go` that wires cobra flags — kept minimal at ~60 LOC).

## 6. Data Flow

For `hbase-metrics-cli rpc-latency --role regionserver --since 10m --top 5 --format table`:

1. `cmd/root.go` parses global flags (`--cluster`, `--vm-url`, `--format`, `--basic-auth-*`, `--timeout`, `--dry-run`).
2. `cmd/scenarios/rpc_latency.go` merges `default_cluster` from config with `--cluster`; validates `--role` enum.
3. `internal/promql.Render("rpc-latency", vars{cluster, role, top, since})` returns a list of rendered queries (one per `queries[]` entry).
4. If `--dry-run`: print envelope with `queries[].expr` only, skip HTTP, exit 0.
5. Else `internal/vmclient.QueryRange(...)` for each query, executed concurrently via `errgroup` with `SetLimit(4)`. Per-request timeout from `--timeout`.
6. Results merged into `Envelope{scenario, cluster, range, queries[], data[]}` keyed by `instance` for table-style scenarios.
7. `internal/output.Render(format, envelope, os.Stdout)`:
   - `json`: full envelope.
   - `table`: `tabwriter`-formatted columns from `columns:` in YAML.
   - `markdown`: same columns as markdown table.
8. Exit codes: `0` success, `1` internal bug, `2` user error (config/flags), `3` VM failure.

### 6.1 JSON envelope

```json
{
  "scenario": "rpc-latency",
  "cluster": "mrs-hbase-oline",
  "range": {"start": "2026-04-28T08:00:00Z", "end": "2026-04-28T08:10:00Z", "step": "30s"},
  "queries": [
    {"label": "p99",  "expr": "topk(5, hadoop_hbase_ipc_processcalltime_99th_percentile{cluster=\"mrs-hbase-oline\", role=\"regionserver\"})"},
    {"label": "p999", "expr": "..."}
  ],
  "data": [
    {"instance": "10.94.0.156:19110", "p99": [[1714291200, "12.3"], ...], "p999": [...]},
    {"instance": "10.94.0.190:19110", "p99": [...], "p999": [...]}
  ]
}
```

`queries[].expr` is the rendered PromQL — Claude Code can use it to drill in further or rewrite the query via the `query` escape hatch.

### 6.2 Config resolution order

```
flag (--vm-url ...) > env (HBASE_VM_*) > ~/.config/hbase-metrics-cli/config.yaml > compile-time default
```

Default `config.yaml`:

```yaml
vm_url: https://vm.rupiahcepatweb.com/
default_cluster: mrs-hbase-oline
basic_auth:
  username: ""
  password: ""
timeout: 10s
```

## 7. Error Handling

All `RunE` functions return `errors.Errorf(code, msg, args...)`. Stderr emits one structured JSON line:

```json
{"error": {"code": "VM_HTTP_5XX", "message": "VictoriaMetrics returned 502", "hint": "check vm.rupiahcepatweb.com reachability or set --timeout 30s"}}
```

stdout stays data-only (so pipes don't corrupt).

| code | trigger | exit |
|---|---|---|
| `CONFIG_MISSING` | no config.yaml and no `--vm-url` | 2 |
| `CONFIG_INVALID` | YAML parse error / invalid URL | 2 |
| `FLAG_INVALID` | `--role` not in enum, `--since` parse fail | 2 |
| `VM_UNREACHABLE` | connect timeout / DNS failure | 3 |
| `VM_HTTP_4XX` | 401/403 (auth), 400 (PromQL error) | 3 |
| `VM_HTTP_5XX` | VM service error | 3 |
| `NO_DATA` | empty result (warning on stderr, exit 0) | 0 |
| `INTERNAL` | bug catch-all | 1 |

`hint` must be actionable. E.g. `VM_HTTP_4XX` 401 → `"set HBASE_VM_USER/HBASE_VM_PASS or run: hbase-metrics-cli config init"`.

## 8. Testing

| layer | scope | tools |
|---|---|---|
| **unit** | `promql.Render`, `output` (3 formats), `config` layered override, `vmclient` URL/auth-header construction | `testing` + `testify`, `httptest.Server` |
| **scenario goldens** | each scenario YAML rendered with fixed vars equals `tests/golden/<scenario>.golden.promql` | golden file with `-update` flag |
| **e2e dry-run** | `hbase-metrics-cli <scenario> --dry-run` for all 12 scenarios; assert stdout JSON contains expected PromQL fragments | runs the built binary, required in CI |
| **e2e live** (optional) | hit real VM, assert `cluster-overview` returns ≥1 row; gated on env var, skipped on fork PRs | runs the built binary |

Not written: per-scenario Go unit tests (golden + dry-run already cover render + end-to-end), VM PromQL engine mocking (mocked queries pass that may fail in production).

## 9. CI Gates

```bash
make unit-test            # go test -race ./...
make e2e-dry              # all 12 scenarios in --dry-run mode
go vet ./...
gofmt -l .                # must produce no output
go mod tidy               # must not change go.mod/go.sum
golangci-lint run
```

Skipped vs `cli/AGENTS.md`: license check (`go-licenses`) — not warranted at this project's scale.

## 10. Claude Code Skill

`.claude/skills/hbase-metrics/SKILL.md` contains:

1. **When to use**: HBase slow / high latency / hot RS / GC / compaction backlog / read-write skew / "show me HBase status".
2. **Prereq check**: `config show` first; if missing, guide `config init`.
3. **Scenario table**: one row per scenario with one-line example.
4. **Common flags**: `--cluster`, `--since`, `--top`, `--format`, `--dry-run`.
5. **Output contract**: stdout = JSON envelope `{scenario, cluster, queries[].expr, data[]}`; stderr = `{error:{code,message,hint}}`; exit codes 0/1/2/3.
6. **Diagnostic playbook** (recommended order for "HBase is slow"):
   1. `cluster-overview` — overall severity
   2. `rpc-latency` + `handler-queue` — service-side bottlenecks
   3. `hotspot-detect` — single-RS hot spot
   4. `gc-pressure` + `jvm-memory` — JVM drag
   5. `compaction-status` + `blockcache-hitrate` — storage layer
   7. Drill in via `queries[].expr`; fall back to `query '<promql>'` when stuck.

## 11. Documentation

- `README.md` (English) and `README.zh.md` (中文) cross-link at top: `[中文版] | [English]`.
- Sections, mirroring lark-cli's structure: Why → Install → Quick Start (Human) → Quick Start (AI Agent) → Scenarios table → Config & Auth → Output formats → Error contract → Contributing.
- AI Agent Quick Start = 4 steps: `go install` → `config init` (interactive: VM URL, default cluster, optional Basic Auth) → symlink `.claude/skills/hbase-metrics` into `~/.claude/skills/` → `cluster-overview` smoke test.
- No CHANGELOG.md / CONTRIBUTING.md — embedded in README.

## 12. Distribution

- `Makefile` targets: `build`, `install` (`go install` to `$GOBIN`), `unit-test`, `e2e-dry`, `lint`, `release-snapshot`.
- `.goreleaser.yml`: darwin/linux × amd64/arm64 (4 binaries) + checksums, triggered on tag push.
- User install paths:
  - Dev: `go install github.com/<owner>/hbase-metrics-cli@latest`
  - Prod: download binary from GitHub Release, drop into `/usr/local/bin/`
  - Skill: `ln -s $PWD/.claude/skills/hbase-metrics ~/.claude/skills/hbase-metrics` (one-liner in README)

## 13. Open Questions

None blocking v1. Future considerations (out of scope):

- Add `cluster_name` (`mrs印尼集群`) as an alias label so users can pass human-readable names.
- Per-table metrics if blacklist is ever lifted — would unlock per-table hotspot detection.
- Caching VM responses for repeated identical queries within a Claude Code session.

## 14. Acceptance Criteria

v1 is done when:

1. `hbase-metrics-cli cluster-overview` against `https://vm.rupiahcepatweb.com/` returns a non-empty JSON envelope on a developer machine with the network reachable.
2. All 12 scenarios pass dry-run e2e in CI.
3. Golden PromQL files exist for all 12 scenarios.
4. `README.md` / `README.zh.md` document install + agent quick start; `SKILL.md` lists all 12 scenarios with examples.
5. CI gates (vet, gofmt, mod tidy, lint, unit, e2e-dry) all green.
6. `goreleaser release --snapshot` produces 4 platform binaries locally.
