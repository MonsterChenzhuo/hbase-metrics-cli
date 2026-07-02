# hbase-logs skill — design

- **Date:** 2026-07-02
- **Status:** approved (design), pending spec review
- **Author:** brainstormed with Claude Code
- **Related:** `hbase-metrics` skill, `hbase-metrics-cli` `query --tz` (2026-07-01)

## Motivation

Incident triage in this fleet is a two-layer job. `hbase-metrics` answers the
metric layer — *when* a symptom happened, *which* RegionServer, *how severe*.
But the root cause almost always lives in the RegionServer log: *which table
was flushing*, *was WAL sync slow*, *which DataNode pipeline stalled*. In recent
investigations that log step was all manual: `awk` a time window out of a
multi-hundred-MB file, `grep` for `Flushing` / `Slow sync`, `sort | uniq -c`
table names by hand, and — every single time — convert between the alarm's
Beijing wall-clock and the log's timezone-less UTC timestamps (and get it wrong
at least once).

[lnav](https://github.com/tstack/lnav) already solves the mechanical parts:
headless SQL over logs, multi-file time-merge, level indexing, custom format
definitions. This skill wraps lnav for the specific shape of Huawei MRS HBase
logs so the agent runs one validated SQL query instead of a pipe of shell
tools, and never hand-computes a timezone offset again.

## Non-goals (YAGNI)

- No real-time `tail` / interactive TUI — headless, after-the-fact analysis only.
- No Go code changes; nothing enters the `hbase-metrics-cli` binary. lnav is an
  optional external dependency, exactly like the "skill teaches the agent to use
  a tool" model of `hbase-metrics`.
- No coverage guarantee for non-HBase components. MRS Master logs share the
  format and reuse the definition; other services are out of scope.
- No log upload / transfer. The skill only reads local files the user provides.

## Positioning

A standalone skill `hbase-logs`, sibling to `hbase-metrics`, complementary:

- **`hbase-metrics`** — metric layer: when, which RS, how severe.
- **`hbase-logs`** — log layer: what that RS was actually doing at that moment,
  which table.

The two chain into one playbook: metrics localizes (RS + peak UTC minute) →
logs drills in (that RS, that minute, which table / WAL / compaction).

## Architecture

The skill teaches the agent to use lnav's **headless SQL mode**
(`lnav -n -c ';<SQL>' <logfile>`). It ships a validated lnav format definition
for MRS HBase logs and encodes the timezone convention as SQL templates.

```
.claude/skills/hbase-logs/
├── SKILL.md                    # agent entry: when to use, pre-flight, query recipes, tz convention, troubleshooting
└── formats/
    └── mrs_hbase_log.json      # lnav format definition (validated against real RS logs)
```

Synced to three locations, matching `hbase-metrics`:
- repo `.claude/skills/hbase-logs/`
- repo `.agents/skills/hbase-logs/`
- user-global `~/.claude/skills/hbase-logs/`

### Log format

Huawei MRS HBase RS/Master log line, pipe-delimited, five fields:

```
2026-07-01 03:55:04,634 | INFO  | regionserver/host:16020.Chore.1 | <message> | org.apache.hadoop.hbase.regionserver.HRegionServer$...(HRegionServer.java:2337)
```

`timestamp | LEVEL | thread | message | class`. Two validated subtleties baked
into the format definition:
- `LEVEL` is space-padded for alignment (`INFO ` / `WARN `) — matched with `\w+\s*`.
- `message` can itself contain `|` — handled with a non-greedy `body` capture
  plus an end-anchored `class` capture so the trailing class field wins.

The `class` and `thread` fields are marked `identifier`; `level-field` maps to
lnav's log levels so `WHERE log_level >= 'warning'` works.

### Timezone convention (the core footgun this skill removes)

**Log timestamps carry no timezone and are UTC. Alarms are Beijing time
(UTC+8).** The skill states this in a prominent box at the top and never asks
the agent to hand-convert. All recipes use SQLite datetime arithmetic:

- Display a UTC log time in Beijing: `datetime(log_time, '+8 hours')`
- Query by a Beijing wall-clock the user gave: `datetime('<beijing>', '-8 hours')`

This mirrors the `--tz` support added to `hbase-metrics-cli query` on
2026-07-01 — same mental model, consistent across metrics and logs.

## Pre-flight (enforced at the top of SKILL.md, like hbase-metrics)

1. `lnav --version` — confirm installed. If absent, prompt
   `brew install lnav`, and note the `HOMEBREW_NO_AUTO_UPDATE=1` workaround for
   the Homebrew-self-update network failure hit during development.
2. Install / confirm the format definition:
   `cp <skill>/formats/mrs_hbase_log.json ~/.lnav/formats/installed/` then
   `lnav -m format mrs_hbase_log get` to validate it loaded.
3. Confirm the user supplied a log file path.

## Query recipes (skill core)

All in `lnav -n -c ';<SQL>' <logfile>` form; validated against real RS logs.

**A. Which table is active in a time window** (replaces manual awk+grep+sort):
```sql
SELECT count(*) n, regexp_match('data/default/(\w+)', log_body) tbl
FROM mrs_hbase_log
WHERE log_time BETWEEN '<UTC start>' AND '<UTC end>' AND log_body LIKE '%data/default/%'
GROUP BY tbl ORDER BY n DESC
```

**B. Query by Beijing time** (auto −8h to query UTC, +8h to display):
```sql
SELECT datetime(log_time,'+8 hours') beijing, log_body FROM mrs_hbase_log
WHERE log_time BETWEEN datetime('<beijing start>','-8 hours') AND datetime('<beijing end>','-8 hours')
  AND log_body LIKE '%Flushing%'
```

**C. WAL / slow-disk evidence** — `Slow sync cost`, extract pipeline DataNodes.

**D. flush / compaction large objects** — `dataSize=`, durations.

**E. Error / exception timeline** — `WHERE log_level >= 'warning'`, per-minute histogram.

**F. Multi-file time-merge** — several RS logs together, `log_path` disambiguates source.

**G. Structured output** — `-c ':write-json-to -'` for the agent, or `:write-csv-to`.

Each recipe is annotated with: purpose, placeholders to replace, and which
`hbase-metrics` step it follows.

### Diagnostic linkage

A short playbook stitching metrics → logs, the exact path recent
investigations took:
1. `hbase-metrics` localizes: which RS, which UTC minute peaked.
2. `hbase-logs` recipe A/B: that RS, that minute → which table was flushing.
3. Recipe C/D: confirm WAL sync spike / compaction as the mechanism.

## Error handling & edge cases (troubleshooting table in SKILL.md)

| Situation | Response |
|---|---|
| lnav not installed | prompt `brew install lnav` (+ `HOMEBREW_NO_AUTO_UPDATE=1` workaround) |
| Format not recognized (SQL empty but file has content) | `;SELECT log_format, count(*) FROM all_logs GROUP BY log_format` to confirm it landed on `mrs_hbase_log`; if not, check the format definition is installed |
| Log detected as another format | ensure the definition is installed with correct priority; verify via the query above |
| Timezone reversed (most common mistake) | prominent box at top fixes "log=UTC, alarm=Beijing(UTC+8)"; every template has ±8h built in |
| Very large log file slow | lnav builds an index on first run; pre-slice the time window with `awk` before feeding lnav |
| message contains a pipe `|` | format definition uses non-greedy body + end-anchored class |

## Testing / validation

Validated during design against two real RS logs from `~/Downloads`
(`20260701061455` = 10.57.0.173, `20260701052142` = 10.57.0.75) with lnav
0.14.0:
- Custom format correctly split thread / message / class.
- Recipe A reproduced the earlier manual finding (`wallet_user_tag` flushing in
  the alarm window) in one query.
- Timezone conversion verified both directions (UTC↔Beijing).
- Multi-file merge + `log_path` source separation confirmed.
- `write-json-to -` produced agent-consumable JSON.

Since this is a skill (docs + one JSON format file, no Go code), there is no
unit-test suite; the acceptance test is that the pre-flight + recipes run
against a supplied MRS HBase log and return correct results.

## Documentation impact

- New skill files in the three sync locations.
- No change to `hbase-metrics-cli` code, `CLAUDE.md` scenario contract, or
  `hbase-metrics` SKILL.md (a one-line cross-reference between the two skills is
  optional, decided at implementation).
