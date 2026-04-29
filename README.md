# hbase-metrics-cli

[中文版](./README.zh.md) | [English](./README.md)

A Go CLI for diagnosing HBase clusters via VictoriaMetrics — built for **Claude Code** and other AI agents. Twelve predefined scenarios output structured JSON; humans can also use `--format table|markdown`.

## Why

- **Agent-Native** — flat 12 commands map 1:1 to common HBase diagnostic questions; output JSON envelope includes the rendered PromQL so the agent can drill in.
- **Single binary** — no Node, no Python; `go install` or download a release.
- **YAML scenarios** — PromQL templates live in `scenarios/*.yaml`, easy to fork and adapt.

## Install

### One-liner (recommended)

Installs the latest release binary into `/usr/local/bin` and the bundled Claude Code skill into `~/.claude/skills/hbase-metrics/`. Re-run the same command to upgrade.

```bash
curl -fsSL https://raw.githubusercontent.com/MonsterChenzhuo/hbase-metrics-cli/main/scripts/install.sh | bash
```

Common overrides:

```bash
# pin a version
curl -fsSL https://raw.githubusercontent.com/MonsterChenzhuo/hbase-metrics-cli/main/scripts/install.sh | VERSION=v0.1.0 bash

# install to a non-sudo path, skip the skill
curl -fsSL https://raw.githubusercontent.com/MonsterChenzhuo/hbase-metrics-cli/main/scripts/install.sh | PREFIX="$HOME/.local/bin" NO_SKILL=1 bash
```

Supported envs: `VERSION`, `PREFIX`, `SKILL_DIR`, `NO_SUDO`, `NO_SKILL`, `REPO`. See `scripts/install.sh` header for details.

### From source

```bash
go install github.com/opay-bigdata/hbase-metrics-cli@latest
```

### From release (manual)

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
| `--since` | unset | Time window (e.g. `30m`, `2h`, `24h`). Range scenarios always use it; instant scenarios use it only if they declare `instant_summary: true` (currently `cluster-overview`). |
| `--step` | `auto` | PromQL step for range queries. `auto` resolves to 30s / 1m / 2m / 5m / 10m by window size. Use `30s` etc. to override. |
| `--raw` | `false` | For range scenarios under `--since`, return the raw datapoint matrix instead of the per-instance summary. |

Per-scenario flags are listed via `hbase-metrics-cli <scenario> --help`.

## Output Contract

**stdout** — JSON envelope (`--format json`):

```json
{
  "scenario": "cluster-overview",
  "cluster": "mrs-hbase-oline",
  "mode": "summary",
  "range": {"start": "...", "end": "...", "step": "5m"},
  "queries": [{"label": "qps_total", "expr": "..."}],
  "columns": ["label", "max", "avg", "p99", "last"],
  "data": [
    {"label": "qps_total", "max": 4321.0, "avg": 1820.5, "p99": 4111.7, "last": 1735.2}
  ]
}
```

`mode` is always one of `instant` (no `--since`), `summary` (default for windowed queries), or `raw` (`--raw`).

**stderr** — structured errors:

```json
{"error": {"code": "VM_HTTP_4XX", "message": "...", "hint": "..."}}
```

**Exit codes:** `0` success / NoData warning · `1` internal · `2` user error · `3` VM failure.

## Real-world example

A live 24-hour diagnostic walk-through (cluster-overview → rpc-latency → hotspot-detect → gc-pressure → blockcache-hitrate, etc.) is captured at [`docs/examples/2026-04-29-24h-cluster-analysis.md`](docs/examples/2026-04-29-24h-cluster-analysis.md). It demonstrates the recommended diagnostic playbook from the bundled Claude Code skill.

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
