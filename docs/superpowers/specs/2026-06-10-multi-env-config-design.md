# hbase-metrics-cli multi-env config — design

**Date:** 2026-06-10
**Status:** implemented in this commit
**Touches:** `internal/config`, `cmd/root.go`, `cmd/configcmd`, docs

## Problem

Operators run hbase-metrics-cli against multiple physical HBase clusters (Nigeria production, Indonesia production, staging, etc.). The original `config.yaml` was flat — one `vm_url` + one `default_cluster`. Switching clusters meant:

- editing `~/.config/hbase-metrics-cli/config.yaml` between runs, **or**
- passing `--vm-url <url> --cluster <label>` every command — error-prone (the two have to match per cluster).

When AI agents drive the CLI, neither option is reliable: agents don't edit shell-state across calls cleanly, and easily forget to update both flags together.

## Goal

Hold multiple named environment profiles in one config file, with a clear default and runtime switch. The flat config must continue to work unchanged; agents must be able to ask which profile is currently effective.

## Design

### Schema

```yaml
active_env: nigeria               # default profile pointer; optional
envs:                             # optional map; empty == old behavior
  nigeria:
    vm_url: http://ng.example.com/
    default_cluster: mrs-hbase-oline-ng
  indonesia:
    vm_url: https://id.example.com/
    default_cluster: mrs-hbase-oline
# flat top-level fields stay valid — fallback when no profile matches
# or when the chosen profile leaves a field empty
vm_url: http://ng.example.com/
default_cluster: mrs-hbase-oline-ng
timeout: 10s
basic_auth:
  username: ""
  password: ""
```

`EnvConfig` carries the same four fields as the flat config (`vm_url`, `default_cluster`, `basic_auth`, `timeout`). Profiles only overlay non-empty fields, so a sparse profile (just `vm_url`) inherits everything else from flat top-level.

### Precedence

**Profile-name selection** (which profile to activate; low → high):

1. `cfg.ActiveEnv` from YAML
2. `HBASE_ENV` env var
3. `--env <name>` flag

**Field-value layering** inside `cmd.LoadEffectiveConfig` (low → high):

```
defaults → file flat → env profile overlay → HBASE_* env vars → --vm-url etc. flags
```

Rationale: profiles are an enhanced default, env vars and flags are immediate operator overrides. The flag-trumps-everything rule from v0.1 is preserved.

### Naming: why `--env`, not `--cluster`

hbase-metrics-cli already uses `--cluster <X>` to mean the PromQL `{cluster="X"}` label value (a metric dimension). The spark-cli precedent uses `active_cluster` / `--cluster` because spark-cli has no such conflict. Reusing `--cluster` for profile selection here would double-bind the flag and break the agent contract.

So:

| Concept | spark-cli | hbase-metrics-cli |
|---|---|---|
| YAML field for default | `active_cluster` | `active_env` |
| YAML map of profiles | `clusters:` | `envs:` |
| Selection flag | `--cluster` | `--env` |
| Env var | (n/a) | `HBASE_ENV` |

### Source tagging

`Sources` gains `SourceEnvProfile = "env_profile"`. When `ApplyEnvProfile` overlays a field, its `Source.*` becomes `env_profile`. A later `HBASE_VM_URL` or `--vm-url` still overrides the value and resets the source to `env` / `flag`. Agents reading `config show` can disambiguate "this came from the active profile" vs "this came from the flat fallback" vs "this was overridden at the command line".

### Errors

- Unknown env name → `CodeConfigInvalid` (exit 2) with a hint listing known names from `EnvNames`.
- Empty name (no `--env`, no `HBASE_ENV`, empty `active_env`, empty `envs:`) → no-op, flat config used as today.

### Out of scope (deferred)

- `config add-env <name>` / `config set-active-env <name>` subcommands. For v1, users edit the YAML directly. Add later if friction is real.
- Interactive `config init` migration that prompts for multiple envs. The existing `config init` continues to write a flat config; that's still a reasonable starting point.
- Per-scenario banner showing the resolved env at every invocation. Adds noise to JSON output; `selected_env` is already accessible via `config show`.

## Test coverage

`internal/config/config_test.go` adds:

- `TestLoad_ReadsEnvsMap` — schema decoding, flat fields untouched.
- `TestApplyEnvProfile_OverridesTopLevel` — overlay sets fields and `SelectedEnv`.
- `TestApplyEnvProfile_UnknownNameErrors` — error path.
- `TestApplyEnvProfile_PreservesUnsetFields` — sparse profile inherits flat values.
- `TestApplyEnvProfile_EmptyNameNoOp` — empty name is safe.
- `TestEnvProfile_FlagOverridesProfileValue` — flag precedence preserved.
- `TestEnvNames_*` — stable sort, empty case.

End-to-end checks live in the implementation plan (`/.claude/plans/goofy-roaming-valley.md`): switching via flag, env var, unknown-name error, flat-config compatibility.

## Files changed

- `internal/config/config.go` — `EnvConfig`, `Config.ActiveEnv/Envs/SelectedEnv`, `SourceEnvProfile`, `ApplyEnvProfile`, `EnvNames`.
- `cmd/root.go` — `--env` flag, `pickEnvName`, new `LoadEffectiveConfig` layer order.
- `cmd/configcmd/configcmd.go` + `show.go` — `New` accepts a `LoadEffectiveFn` so `config show` runs through the full layering and reports `active_env` / `selected_env` / `envs`.
- `internal/config/config_test.go` — test cases above.
- `CLAUDE.md`, `.claude/skills/hbase-metrics/SKILL.md`, `CHANGELOG.md` — doc sync.
