---
name: hbase-logs
description: Use when analyzing Huawei MRS HBase RegionServer/Master log files a user provides — to find which table was flushing, WAL sync slowness, compaction bursts, or an error timeline in a specific time window. Wraps lnav headless SQL; complements the hbase-metrics skill (metrics localizes the RS + minute, logs drill into what happened).
---

# HBase Logs Skill

Analyze Huawei MRS HBase RS/Master logs with lnav's headless SQL mode. This is
the log-layer companion to `hbase-metrics`: metrics tells you *which RS* and
*which minute*; this skill tells you *what that RS was doing* — which table
flushed, whether WAL sync stalled, which DataNode pipeline was slow.

## When to use
- User provides an HBase RegionServer / Master log file (or a directory of them)
  and asks what happened in a time window.
- You just localized an incident with `hbase-metrics` (an RS + a UTC minute) and
  need to confirm the root cause in that RS's log.
- Questions like "which table caused it", "was it read or write", "why did RPC
  p999 spike", "find the errors around <time>".

## ⚠️ Timezone convention (read first — this is the #1 mistake)
**MRS HBase log timestamps carry NO timezone and are UTC. Alarms are Beijing
time (UTC+8).** Never hand-convert. Use SQLite datetime arithmetic in every query:
- Show a UTC log time in Beijing: `datetime(log_time, '+8 hours')`
- Query by a Beijing wall-clock the user gave: `datetime('<beijing>', '-8 hours')`

So an alarm at Beijing `11:57` is log-UTC `03:57`. When in doubt, SELECT both
`log_time` (UTC) and `datetime(log_time,'+8 hours')` (Beijing) so the mapping is
visible in the output.

## Pre-flight check (run first)
1. `lnav --version` — confirm lnav is installed. If missing, tell the user to
   run `brew install lnav`. If brew fails trying to self-update (network), retry
   with `HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_FROM_API=1 brew install lnav`.
2. Install the MRS format definition (idempotent), then verify it loads:
   ```bash
   mkdir -p ~/.lnav/formats/installed
   cp "$SKILL_DIR/formats/mrs_hbase_log.json" ~/.lnav/formats/installed/
   lnav -m format mrs_hbase_log get
   ```
   `$SKILL_DIR` is this skill's directory. Expected: it prints the format title.
3. Confirm the user gave a log file path. All queries below take one or more
   file paths as the final argument(s) to `lnav`.

## How queries work
Every recipe is headless: `lnav -n -c ';<SQL>' <logfile> [<logfile> ...]`.
The table is `mrs_hbase_log` with columns: `log_time` (UTC), `log_level`
(`info`/`warning`/`error`), `thread`, `log_body` (the message), `class`,
`log_path` (source file, for multi-file merges). For agent-consumable output,
append `-c ':write-json-to -'` (JSON) or `-c ':write-csv-to -'` (CSV).
