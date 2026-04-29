# hbase-metrics-cli

[中文版](./README.zh.md) | [English](./README.md)

面向 **Claude Code** 等 AI Agent 的 HBase 监控诊断 CLI（Go 实现）—— 通过 VictoriaMetrics 查询 12 个预定义场景，默认输出结构化 JSON，方便 Agent 解析；人也可以 `--format table|markdown`。

## 为什么

- **面向 Agent** —— 12 个扁平命令 1:1 对应 HBase 常见诊断问题；JSON envelope 同时回吐渲染后的 PromQL，方便 Agent 钻取。
- **单二进制** —— 不依赖 Node / Python；`go install` 或下载 release。
- **场景即 YAML** —— PromQL 模板放在 `scenarios/*.yaml`，可 fork 改写。

## 安装

### 一键安装（推荐）

下载最新 release 二进制到 `/usr/local/bin`，并把内置 Claude Code skill 安装到 `~/.claude/skills/hbase-metrics/`。重复执行即升级。

```bash
curl -fsSL https://raw.githubusercontent.com/MonsterChenzhuo/hbase-metrics-cli/main/scripts/install.sh | bash
```

常见覆盖参数：

```bash
# 锁定版本
curl -fsSL https://raw.githubusercontent.com/MonsterChenzhuo/hbase-metrics-cli/main/scripts/install.sh | VERSION=v0.1.0 bash

# 装到无需 sudo 的目录、跳过 skill
curl -fsSL https://raw.githubusercontent.com/MonsterChenzhuo/hbase-metrics-cli/main/scripts/install.sh | PREFIX="$HOME/.local/bin" NO_SKILL=1 bash
```

可用环境变量：`VERSION`、`PREFIX`、`SKILL_DIR`、`NO_SUDO`、`NO_SKILL`、`REPO`，详见 `scripts/install.sh` 头部注释。

### 源码安装

```bash
go install github.com/opay-bigdata/hbase-metrics-cli@latest
```

### 手动二进制安装

从 GitHub Releases 下载对应操作系统的 archive，`tar -xzf … && mv hbase-metrics-cli /usr/local/bin/`。

## 快速上手（人类）

```bash
# 1. 配置（交互式）
hbase-metrics-cli config init

# 2. 校验
hbase-metrics-cli config show

# 3. 诊断
hbase-metrics-cli cluster-overview --format table
```

## 快速上手（AI Agent / Claude Code）

```bash
# 1. 安装二进制
go install github.com/opay-bigdata/hbase-metrics-cli@latest

# 2. 预置配置（非交互）
mkdir -p ~/.config/hbase-metrics-cli
cat > ~/.config/hbase-metrics-cli/config.yaml <<EOF
vm_url: https://vm.example.com/
default_cluster: mrs-hbase-oline
timeout: 10s
EOF

# 3. 安装 Claude Code Skill
ln -sf "$(pwd)/.claude/skills/hbase-metrics" ~/.claude/skills/hbase-metrics

# 4. 自检
hbase-metrics-cli cluster-overview --format json
```

## 12 个场景

| 命令 | 适用情境 |
|---|---|
| `cluster-overview` | 集群整体状态 |
| `regionserver-list` | 各 RS 的 region 数 / QPS / heap |
| `requests-qps` | 读 / 写 / 总 QPS 时间序列 |
| `rpc-latency` | RPC P99 / P999 Top-K |
| `handler-queue` | IPC handler 队列堆积 |
| `hotspot-detect` | 单 RS 热点检测 |
| `gc-pressure` | GC 频率 / 暂停 |
| `jvm-memory` | 堆使用率趋势 |
| `compaction-status` | compaction 队列堆积 |
| `blockcache-hitrate` | BlockCache 命中率 / 大小 |
| `wal-stats` | WAL 慢 append / sync P99 |
| `master-status` | Master 状态、RIT、平均负载 |
| `query '<promql>'` | 兜底原始 PromQL |

## 通用参数

| Flag | 默认 | 说明 |
|---|---|---|
| `--vm-url` | 来自 config | 覆盖 `HBASE_VM_URL` |
| `--cluster` | 来自 config | cluster 标签值 |
| `--basic-auth-user` / `--basic-auth-pass` | 空 | 或 `HBASE_VM_USER` / `HBASE_VM_PASS` |
| `--timeout` | `10s` | HTTP 超时 |
| `--format` | `json` | `json` / `table` / `markdown` |
| `--dry-run` | `false` | 仅打印渲染后的 PromQL，不发请求 |
| `--since` | 未设 | 时间窗口（如 `30m` / `2h` / `24h`）。range 场景始终生效；instant 场景仅当声明 `instant_summary: true` 时生效（目前为 `cluster-overview`）。 |
| `--step` | `auto` | range 查询步长。`auto` 按窗口大小自动选取 30s / 1m / 2m / 5m / 10m。可显式覆盖。 |
| `--raw` | `false` | range 场景配合 `--since` 时返回原始数据点矩阵，跳过聚合摘要。 |

各场景独有参数：`hbase-metrics-cli <scenario> --help`。

## 输出契约

**stdout** —— JSON envelope（`--format json`）：

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

`mode` 三选一：`instant`（无 `--since`）/ `summary`（窗口查询默认）/ `raw`（`--raw`）。

**stderr** —— 结构化错误：

```json
{"error": {"code": "VM_HTTP_4XX", "message": "...", "hint": "..."}}
```

**退出码：** `0` 成功 / NoData 警告 · `1` 内部错 · `2` 用户错 · `3` VM 故障。

## 真实案例

一份完整 24 小时诊断（cluster-overview → rpc-latency → hotspot-detect → gc-pressure → blockcache-hitrate 等）见 [`docs/examples/2026-04-29-24h-cluster-analysis.md`](docs/examples/2026-04-29-24h-cluster-analysis.md)，演示了内置 Claude Code skill 推荐的诊断 playbook。

## 配置文件

`~/.config/hbase-metrics-cli/config.yaml`（也可通过 `$HBASE_METRICS_CLI_CONFIG_DIR` 自定义目录）：

```yaml
vm_url: https://vm.example.com/
default_cluster: mrs-hbase-oline
basic_auth:
  username: ""
  password: ""
timeout: 10s
```

覆盖优先级（高到低）：`flag` > `env` > `config.yaml` > 编译期默认。

## 开发

```bash
make unit-test     # go test -race ./...
make e2e-dry       # 12 个场景 dry-run e2e
make lint          # vet + gofmt + golangci-lint
make build         # 产出 ./hbase-metrics-cli
```

## License

MIT。
