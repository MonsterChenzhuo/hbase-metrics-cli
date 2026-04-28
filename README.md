# hbase-metrics-cli

[中文版](./README.zh.md) | [English](./README.md)

A Go CLI for diagnosing HBase clusters via VictoriaMetrics — built for **Claude Code** and other AI agents. Twelve predefined scenarios output structured JSON; humans can also use `--format table|markdown`.

## Why

- **Agent-Native** — flat 12 commands map 1:1 to common HBase diagnostic questions; output JSON envelope includes the rendered PromQL so the agent can drill in.
- **Single binary** — no Node, no Python; `go install` or download a release.
- **YAML scenarios** — PromQL templates live in `scenarios/*.yaml`, easy to fork and adapt.

## Install

### From source

```bash
go install github.com/opay-bigdata/hbase-metrics-cli@latest
```

### From release

Download the archive for your OS from GitHub Releases, then `tar -xzf … && mv hbase-metrics-cli /usr/local/bin/`.

## Quick Start (Human)

```bash
# 1. Configure (interactive)
hbase-metrics-cli config init

# 2. Check
hbase-metrics-cli config show

# 3. Diagnose
hbase-metrics-cli cluster-overview --format table
```

## Quick Start (AI Agent / Claude Code)

```bash
# 1. Install binary
go install github.com/opay-bigdata/hbase-metrics-cli@latest

# 2. Pre-fill config (non-interactive)
mkdir -p ~/.config/hbase-metrics-cli
cat > ~/.config/hbase-metrics-cli/config.yaml <<EOF
vm_url: https://vm.example.com/
default_cluster: mrs-hbase-oline
timeout: 10s
EOF

# 3. Install Skill
ln -sf "$(pwd)/.claude/skills/hbase-metrics" ~/.claude/skills/hbase-metrics

# 4. Smoke
hbase-metrics-cli cluster-overview --format json
```

## Scenarios (12)

| Command | Use when |
|---|---|
| `cluster-overview` | "How is the cluster overall?" |
| `regionserver-list` | "Which RS holds how many regions / what QPS?" |
| `requests-qps` | "Read vs write QPS over time?" |
| `rpc-latency` | "RPC P99 / P999 — top offenders?" |
| `handler-queue` | "IPC handlers backed up?" |
| `hotspot-detect` | "Single-RS hotspot?" |
| `gc-pressure` | "GC frequency / pause too high?" |
| `jvm-memory` | "Heap usage trend?" |
| `compaction-status` | "Compaction backlog?" |
| `blockcache-hitrate` | "BlockCache effective?" |
| `wal-stats` | "WAL slow appends / sync latency?" |
| `master-status` | "Master state, RIT count, average load?" |
| `query '<promql>'` | Escape hatch for raw PromQL |

## Common Flags

| Flag | Default | Notes |
|---|---|---|
| `--vm-url` | from config | Overrides `HBASE_VM_URL` |
| `--cluster` | from config | Cluster label value |
| `--basic-auth-user` / `--basic-auth-pass` | empty | Or `HBASE_VM_USER` / `HBASE_VM_PASS` |
| `--timeout` | `10s` | HTTP timeout |
| `--format` | `json` | `json` / `table` / `markdown` |
| `--dry-run` | `false` | Print rendered PromQL, skip HTTP |

Per-scenario flags are listed via `hbase-metrics-cli <scenario> --help`.

## Output Contract

**stdout** — JSON envelope (`--format json`):

```json
{
  "scenario": "rpc-latency",
  "cluster": "mrs-hbase-oline",
  "range": {"start": "...", "end": "...", "step": "30s"},
  "queries": [{"label": "p99", "expr": "topk(10, ...)"}],
  "columns": ["instance", "p99", "p999"],
  "data": [{"instance": "10.0.0.1:19110", "p99": 12.3, "p999": 25.1}]
}
```

**stderr** — structured errors:

```json
{"error": {"code": "VM_HTTP_4XX", "message": "...", "hint": "..."}}
```

**Exit codes:** `0` success / NoData warning · `1` internal · `2` user error · `3` VM failure.

## Configuration

`~/.config/hbase-metrics-cli/config.yaml` (also via `$HBASE_METRICS_CLI_CONFIG_DIR`):

```yaml
vm_url: https://vm.example.com/
default_cluster: mrs-hbase-oline
basic_auth:
  username: ""
  password: ""
timeout: 10s
```

Resolution order (highest wins): `flag` > `env` > `config.yaml` > built-in default.

## Development

```bash
make unit-test     # go test -race ./...
make e2e-dry       # all 12 scenarios in --dry-run mode
make lint          # vet + gofmt + golangci-lint
make build         # produce ./hbase-metrics-cli
```

## License

MIT.
