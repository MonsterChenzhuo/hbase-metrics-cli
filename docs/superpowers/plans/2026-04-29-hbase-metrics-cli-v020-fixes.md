# hbase-metrics-cli v0.2.0 Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement v0.2.0 of `hbase-metrics-cli`: default summary aggregation for time-range scenarios, hybrid `cluster-overview --since`, `--step auto`, counter-reset hardening in PromQL, field naming unification, and a documented 24h analysis example.

**Architecture:** Two new pure-Go internal packages (`aggregate`, `stepauto`) feed a refactored `cmd/scenarios/runner.go` that routes between `instant` / `summary` / `raw` modes; YAML scenarios gain optional `instant_summary` / `summary_columns` / `summary` blocks; PromQL changed from `[1m]` to `clamp_min(...[5m], 0)` for counter-reset prone metrics; `cluster-overview` uses `max(...)` instead of `count(...)` for the gauge metric.

**Tech Stack:** Go 1.23+, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`, `golang.org/x/sync/errgroup`, `github.com/stretchr/testify`, golangci-lint v2.

**Reference spec:** `docs/superpowers/specs/2026-04-29-hbase-metrics-cli-fixes-design.md`

**Working directory for all tasks:** `/Users/opay-20240095/IdeaProjects/createcli/hbase-metrics-cli`

**Module path:** `github.com/opay-bigdata/hbase-metrics-cli`

**Convention:** every task ends with `make tidy && make lint && make unit-test` green and a single commit. Use `make e2e-dry` only on tasks that touch scenario YAML or runner.

---

## Task 1: `internal/aggregate` — pure summary math

**Files:**
- Create: `internal/aggregate/summary.go`
- Create: `internal/aggregate/summary_test.go`

- [ ] **Step 1: Write failing tests**

`internal/aggregate/summary_test.go`:

```go
package aggregate

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func datapoints(vals ...string) [][2]any {
	out := make([][2]any, len(vals))
	for i, v := range vals {
		out[i] = [2]any{float64(1_700_000_000 + i*30), v}
	}
	return out
}

func TestSummarize_Empty(t *testing.T) {
	s := Summarize(nil)
	require.Equal(t, 0, s.Count)
	require.Zero(t, s.Max)
	require.Zero(t, s.Avg)
}

func TestSummarize_SinglePoint(t *testing.T) {
	s := Summarize(datapoints("42"))
	require.Equal(t, 1, s.Count)
	require.Equal(t, 42.0, s.Min)
	require.Equal(t, 42.0, s.Max)
	require.Equal(t, 42.0, s.Avg)
	require.Equal(t, 42.0, s.P50)
	require.Equal(t, 42.0, s.P99)
	require.Equal(t, 42.0, s.Last)
}

func TestSummarize_OrderStatistics(t *testing.T) {
	// 1..100 → p50≈50, p95≈95, p99≈99, max=100
	vals := make([]string, 100)
	for i := 0; i < 100; i++ {
		vals[i] = fmtFloat(float64(i + 1))
	}
	s := Summarize(datapoints(vals...))
	require.Equal(t, 100, s.Count)
	require.Equal(t, 1.0, s.Min)
	require.Equal(t, 100.0, s.Max)
	require.InDelta(t, 50.5, s.Avg, 0.01)
	require.InDelta(t, 50.0, s.P50, 1.0)
	require.InDelta(t, 95.0, s.P95, 1.0)
	require.InDelta(t, 99.0, s.P99, 1.0)
	require.Equal(t, 100.0, s.Last)
}

// The live-cluster reset-spike fixture from the 2026-04-29 24h analysis:
// most samples around 200/s, one outlier at 13129/s (counter reset).
func TestSummarize_ResetSpike(t *testing.T) {
	vals := make([]string, 100)
	for i := range vals {
		vals[i] = "200"
	}
	vals[42] = "13129"
	s := Summarize(datapoints(vals...))
	require.Equal(t, 13129.0, s.Max)
	require.InDelta(t, 200.0, s.P99, 1.0, "p99 must not be polluted by the reset spike")
	require.InDelta(t, 200.0, s.P50, 1.0)
}

func TestSummarize_NaNAndInfExcluded(t *testing.T) {
	s := Summarize(datapoints("10", "NaN", "+Inf", "20", "30"))
	require.Equal(t, 5, s.Count)
	require.Equal(t, 10.0, s.Min)
	require.Equal(t, 30.0, s.Max)
	require.InDelta(t, 20.0, s.Avg, 0.01) // (10+20+30)/3
	require.InDelta(t, 0.4, s.NaNRatio, 0.01)
}

func TestSummarize_AllInvalid(t *testing.T) {
	s := Summarize(datapoints("NaN", "+Inf"))
	require.Equal(t, 2, s.Count)
	require.Equal(t, 1.0, s.NaNRatio)
	require.True(t, math.IsNaN(s.Avg) || s.Avg == 0)
}

func fmtFloat(f float64) string {
	return strconvFmt(f)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/opay-20240095/IdeaProjects/createcli/hbase-metrics-cli
go test ./internal/aggregate/...
```

Expected: build error — package does not exist yet.

- [ ] **Step 3: Implement `summary.go`**

`internal/aggregate/summary.go`:

```go
// Package aggregate turns VictoriaMetrics range datapoints into fixed Summary
// scalars (min/max/avg/p50/p95/p99/last). It knows nothing about scenarios,
// HTTP, or output formatting — boundary contract per CLAUDE.md.
package aggregate

import (
	"math"
	"sort"
	"strconv"
)

type Summary struct {
	Count    int     `json:"count"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Avg      float64 `json:"avg"`
	P50      float64 `json:"p50"`
	P95      float64 `json:"p95"`
	P99      float64 `json:"p99"`
	Last     float64 `json:"last"`
	NaNRatio float64 `json:"nan_ratio,omitempty"`
}

// Summarize computes order statistics over a VictoriaMetrics datapoint slice.
// `values[i]` is `[unix_seconds, "value-string"]`. NaN / +Inf / -Inf are
// excluded from numeric stats but counted in NaNRatio. Returns Summary{Count:0}
// on empty input.
func Summarize(values [][2]any) Summary {
	if len(values) == 0 {
		return Summary{}
	}
	nums := make([]float64, 0, len(values))
	var lastValid float64
	var sawValid bool
	invalid := 0
	for _, v := range values {
		s, ok := v[1].(string)
		if !ok {
			invalid++
			continue
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			invalid++
			continue
		}
		nums = append(nums, f)
		lastValid = f
		sawValid = true
	}
	out := Summary{
		Count:    len(values),
		NaNRatio: float64(invalid) / float64(len(values)),
	}
	if !sawValid {
		return out
	}
	sorted := make([]float64, len(nums))
	copy(sorted, nums)
	sort.Float64s(sorted)
	out.Min = sorted[0]
	out.Max = sorted[len(sorted)-1]
	var sum float64
	for _, n := range nums {
		sum += n
	}
	out.Avg = sum / float64(len(nums))
	out.P50 = quantile(sorted, 0.50)
	out.P95 = quantile(sorted, 0.95)
	out.P99 = quantile(sorted, 0.99)
	out.Last = lastValid
	return out
}

// quantile uses the nearest-rank method on a pre-sorted slice.
func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(q*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// strconvFmt is exposed for the test helper to keep imports symmetric.
func strconvFmt(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/aggregate/... -race -count=1 -v
```

Expected: all 6 tests PASS.

- [ ] **Step 5: Run `make tidy && make lint`**

```bash
make tidy && make lint
```

Expected: no diff, no lint errors.

- [ ] **Step 6: Commit**

```bash
git add internal/aggregate/
git commit -m "feat(aggregate): add Summarize for time-range datapoint reduction

Pure-Go module that converts VictoriaMetrics range result datapoints into
fixed Summary scalars (min/max/avg/p50/p95/p99/last) for the upcoming
default-summary mode. Reset-spike resilience verified by a fixture matching
the 2026-04-29 live-cluster observation (max=13129, p99≈200).

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 2: `internal/stepauto` — auto step resolver

**Files:**
- Create: `internal/stepauto/stepauto.go`
- Create: `internal/stepauto/stepauto_test.go`

- [ ] **Step 1: Write failing tests**

`internal/stepauto/stepauto_test.go`:

```go
package stepauto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolve_Boundaries(t *testing.T) {
	cases := []struct {
		name  string
		since time.Duration
		want  time.Duration
	}{
		{"5m", 5 * time.Minute, 30 * time.Second},
		{"30m exact", 30 * time.Minute, 30 * time.Second},
		{"31m", 31 * time.Minute, 1 * time.Minute},
		{"2h exact", 2 * time.Hour, 1 * time.Minute},
		{"3h", 3 * time.Hour, 2 * time.Minute},
		{"12h exact", 12 * time.Hour, 2 * time.Minute},
		{"24h exact", 24 * time.Hour, 5 * time.Minute},
		{"7d", 7 * 24 * time.Hour, 10 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, Resolve(c.since))
		})
	}
}

func TestResolve_Zero(t *testing.T) {
	// zero/negative since: defensive fallback to 30s
	require.Equal(t, 30*time.Second, Resolve(0))
	require.Equal(t, 30*time.Second, Resolve(-time.Hour))
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/stepauto/...
```

Expected: build error — package does not exist.

- [ ] **Step 3: Implement `stepauto.go`**

`internal/stepauto/stepauto.go`:

```go
// Package stepauto resolves a sensible PromQL `step` from a `since` window.
package stepauto

import "time"

// Resolve returns the step matching the time window. Caller-supplied step
// (e.g. --step 30s) takes priority over this; this function is only consulted
// when --step is "auto" or unset.
func Resolve(since time.Duration) time.Duration {
	switch {
	case since <= 0:
		return 30 * time.Second
	case since <= 30*time.Minute:
		return 30 * time.Second
	case since <= 2*time.Hour:
		return 1 * time.Minute
	case since <= 12*time.Hour:
		return 2 * time.Minute
	case since <= 24*time.Hour:
		return 5 * time.Minute
	default:
		return 10 * time.Minute
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/stepauto/... -race -count=1 -v
```

Expected: all 9 sub-tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/stepauto/
git commit -m "feat(stepauto): add Resolve for --step auto resolution

Maps the --since window to a sensible PromQL step so that 24h queries no
longer return 2880 datapoints per series. Boundary cases pinned by tests.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 3: Extend `promql.Scenario` with summary schema

**Files:**
- Modify: `internal/promql/scenarios.go`
- Modify: `internal/promql/promql_test.go` (extend)
- Test: `internal/promql/promql_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/promql/promql_test.go`:

```go
func TestParseScenario_SummaryFields(t *testing.T) {
	yamlStr := `
name: my-scenario
description: test
range: true
defaults:
  since: 5m
  step: 30s
instant_summary: false
summary_columns: [instance, foo_max, foo_p99]
summary:
  foo:
    aggs: [max, p99]
columns: [instance, foo]
queries:
  - label: foo
    expr: rate(x[1m])
`
	s, err := ParseScenario([]byte(yamlStr))
	require.NoError(t, err)
	require.False(t, s.InstantSummary)
	require.Equal(t, []string{"instance", "foo_max", "foo_p99"}, s.SummaryColumns)
	require.Contains(t, s.Summary, "foo")
	require.Equal(t, []string{"max", "p99"}, s.Summary["foo"].Aggs)
}

func TestParseScenario_InstantSummaryTrue(t *testing.T) {
	yamlStr := `
name: cluster-overview
description: test
range: false
instant_summary: true
columns: [label, value]
queries:
  - label: foo
    expr: max(x)
`
	s, err := ParseScenario([]byte(yamlStr))
	require.NoError(t, err)
	require.True(t, s.InstantSummary)
}

func TestParseScenario_RejectsUnknownAgg(t *testing.T) {
	yamlStr := `
name: bad
description: test
range: true
columns: [instance, foo]
summary:
  foo:
    aggs: [max, garbage]
queries:
  - label: foo
    expr: x
`
	_, err := ParseScenario([]byte(yamlStr))
	require.Error(t, err)
	require.Contains(t, err.Error(), "garbage")
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/promql/... -run TestParseScenario_Summary
go test ./internal/promql/... -run TestParseScenario_Instant
go test ./internal/promql/... -run TestParseScenario_RejectsUnknownAgg
```

Expected: build error — `InstantSummary`, `SummaryColumns`, `Summary` not defined.

- [ ] **Step 3: Extend the `Scenario` struct and validation**

Modify `internal/promql/scenarios.go`:

Replace the `Scenario` struct (currently lines 27-35) with:

```go
type SummarySpec struct {
	Aggs []string `yaml:"aggs"`
}

type Scenario struct {
	Name           string                 `yaml:"name"`
	Description    string                 `yaml:"description"`
	Range          bool                   `yaml:"range"`
	InstantSummary bool                   `yaml:"instant_summary"`
	Defaults       map[string]any         `yaml:"defaults"`
	Flags          []Flag                 `yaml:"flags"`
	Queries        []Query                `yaml:"queries"`
	Columns        []string               `yaml:"columns"`
	SummaryColumns []string               `yaml:"summary_columns"`
	Summary        map[string]SummarySpec `yaml:"summary"`
}
```

Replace `ParseScenario` (currently lines 67-79) with:

```go
var validAggs = map[string]struct{}{
	"count": {}, "min": {}, "max": {}, "avg": {},
	"p50": {}, "p95": {}, "p99": {}, "last": {},
}

func ParseScenario(b []byte) (Scenario, error) {
	var s Scenario
	if err := yaml.Unmarshal(b, &s); err != nil {
		return Scenario{}, err
	}
	if s.Name == "" {
		return Scenario{}, fmt.Errorf("scenario missing name")
	}
	if len(s.Queries) == 0 {
		return Scenario{}, fmt.Errorf("scenario %s has no queries", s.Name)
	}
	for label, spec := range s.Summary {
		for _, agg := range spec.Aggs {
			if _, ok := validAggs[agg]; !ok {
				return Scenario{}, fmt.Errorf("scenario %s query %q: unknown agg %q (valid: count,min,max,avg,p50,p95,p99,last)", s.Name, label, agg)
			}
		}
	}
	return s, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/promql/... -race -count=1 -v
```

Expected: all 3 new tests PASS, existing tests still PASS.

- [ ] **Step 5: Run `make tidy && make lint && make unit-test`**

```bash
make tidy && make lint && make unit-test
```

Expected: green.

- [ ] **Step 6: Commit**

```bash
git add internal/promql/
git commit -m "feat(promql): add Scenario.InstantSummary, SummaryColumns, Summary

Extends the YAML schema with optional summary aggregation hints. ParseScenario
rejects unknown agg keys. No runtime behavior change yet — runner.go consumes
these fields in a later task.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 4: Add `Mode` to `output.Envelope`

**Files:**
- Modify: `internal/output/output.go`
- Modify: `internal/output/output_test.go`

- [ ] **Step 1: Write failing test**

Append to `internal/output/output_test.go`:

```go
func TestEnvelope_ModeSerialized(t *testing.T) {
	env := Envelope{
		Scenario: "x",
		Cluster:  "c",
		Mode:     "summary",
		Queries:  []Query{{Label: "l", Expr: "e"}},
		Columns:  []string{"a"},
		Data:     []Row{{"a": 1}},
	}
	var buf strings.Builder
	require.NoError(t, Render("json", env, &buf))
	require.Contains(t, buf.String(), `"mode": "summary"`)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/output/... -run TestEnvelope_ModeSerialized
```

Expected: build error — `Envelope.Mode` not defined.

- [ ] **Step 3: Add `Mode` field**

Modify `internal/output/output.go` `Envelope` struct (currently lines 27-34):

```go
type Envelope struct {
	Scenario string   `json:"scenario"`
	Cluster  string   `json:"cluster"`
	Mode     string   `json:"mode"`
	Range    *Range   `json:"range,omitempty"`
	Queries  []Query  `json:"queries"`
	Columns  []string `json:"columns,omitempty"`
	Data     []Row    `json:"data"`
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/output/... -race -count=1 -v
```

Expected: new test PASS, existing tests still PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/output/
git commit -m "feat(output): add Envelope.Mode field

The runner will populate Mode with one of \"instant\", \"summary\", or \"raw\"
so JSON consumers can branch deterministically.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 5: Runner — `summarizeByInstance` helper

**Files:**
- Create: `cmd/scenarios/summarize.go`
- Create: `cmd/scenarios/summarize_test.go`

- [ ] **Step 1: Write failing tests**

`cmd/scenarios/summarize_test.go`:

```go
package scenarios

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

func sample(instance string, vals ...string) vmclient.Sample {
	out := make([][]any, len(vals))
	for i, v := range vals {
		out[i] = []any{float64(1_700_000_000 + i*30), v}
	}
	return vmclient.Sample{Metric: map[string]string{"instance": instance}, Values: out}
}

func TestSummarizeByInstance_DefaultAggs(t *testing.T) {
	rendered := []promql.Rendered{{Label: "qps"}}
	results := []vmclient.Result{
		{Result: []vmclient.Sample{sample("rs1", "100", "200", "300")}},
	}
	scenario := promql.Scenario{Name: "test", Queries: []promql.Query{{Label: "qps"}}}

	rows := summarizeByInstance(scenario, rendered, results)
	require.Len(t, rows, 1)
	require.Equal(t, "rs1", rows[0]["instance"])
	require.Equal(t, 300.0, rows[0]["qps_max"])
	require.InDelta(t, 200.0, rows[0]["qps_avg"], 0.01)
	require.Equal(t, 300.0, rows[0]["qps_p99"])
	require.Equal(t, 300.0, rows[0]["qps_last"])
}

func TestSummarizeByInstance_CustomAggs(t *testing.T) {
	rendered := []promql.Rendered{{Label: "lat"}}
	results := []vmclient.Result{
		{Result: []vmclient.Sample{sample("rs1", "10", "20", "30")}},
	}
	scenario := promql.Scenario{
		Name:    "test",
		Queries: []promql.Query{{Label: "lat"}},
		Summary: map[string]promql.SummarySpec{"lat": {Aggs: []string{"max", "p50"}}},
	}

	rows := summarizeByInstance(scenario, rendered, results)
	require.Len(t, rows, 1)
	require.Equal(t, 30.0, rows[0]["lat_max"])
	require.NotContains(t, rows[0], "lat_avg")
	require.NotContains(t, rows[0], "lat_p99")
	require.Contains(t, rows[0], "lat_p50")
}

func TestSummarizeByInstance_LabelValueMode(t *testing.T) {
	// cluster-overview style: label-value scenario in summary mode
	rendered := []promql.Rendered{{Label: "qps_total"}, {Label: "regions_total"}}
	results := []vmclient.Result{
		{Result: []vmclient.Sample{{Metric: map[string]string{}, Values: [][]any{
			{float64(1_700_000_000), "10"},
			{float64(1_700_000_030), "20"},
			{float64(1_700_000_060), "30"},
		}}}},
		{Result: []vmclient.Sample{{Metric: map[string]string{}, Values: [][]any{
			{float64(1_700_000_000), "100"},
			{float64(1_700_000_030), "100"},
			{float64(1_700_000_060), "100"},
		}}}},
	}
	scenario := promql.Scenario{
		Name:           "cluster-overview",
		Columns:        []string{"label", "value"},
		Queries:        []promql.Query{{Label: "qps_total"}, {Label: "regions_total"}},
	}

	rows := summarizeLabelValue(scenario, rendered, results)
	require.Len(t, rows, 2)
	require.Equal(t, "qps_total", rows[0]["label"])
	require.Equal(t, 30.0, rows[0]["max"])
	require.InDelta(t, 20.0, rows[0]["avg"], 0.01)
	require.Equal(t, 30.0, rows[0]["last"])
	require.Equal(t, "regions_total", rows[1]["label"])
	require.Equal(t, 100.0, rows[1]["max"])
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./cmd/scenarios/... -run TestSummarize
```

Expected: build error — helpers don't exist.

- [ ] **Step 3: Implement `summarize.go`**

`cmd/scenarios/summarize.go`:

```go
package scenarios

import (
	"github.com/opay-bigdata/hbase-metrics-cli/internal/aggregate"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

var defaultAggs = []string{"max", "avg", "p99", "last"}

// aggsFor returns the aggregation list for a query, falling back to defaults.
func aggsFor(scenario promql.Scenario, queryLabel string) []string {
	if spec, ok := scenario.Summary[queryLabel]; ok && len(spec.Aggs) > 0 {
		return spec.Aggs
	}
	return defaultAggs
}

// summarizeByInstance produces one row per instance with `<label>_<agg>` columns.
func summarizeByInstance(scenario promql.Scenario, rendered []promql.Rendered, results []vmclient.Result) []output.Row {
	rows := map[string]output.Row{}
	for i, res := range results {
		label := rendered[i].Label
		aggs := aggsFor(scenario, label)
		for _, sample := range res.Result {
			key := sample.Metric["instance"]
			if key == "" {
				continue
			}
			row, ok := rows[key]
			if !ok {
				row = output.Row{"instance": key}
				rows[key] = row
			}
			values := toDatapoints(sample.Values)
			s := aggregate.Summarize(values)
			for _, agg := range aggs {
				row[label+"_"+agg] = pickAgg(s, agg)
			}
		}
	}
	out := make([]output.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}
	return out
}

// summarizeLabelValue produces one row per query with `label` + agg columns —
// used for label-value scenarios (e.g. cluster-overview) in --since mode.
func summarizeLabelValue(scenario promql.Scenario, rendered []promql.Rendered, results []vmclient.Result) []output.Row {
	rows := make([]output.Row, 0, len(rendered))
	for i, res := range results {
		label := rendered[i].Label
		aggs := aggsFor(scenario, label)
		row := output.Row{"label": label}
		if len(res.Result) > 0 {
			values := toDatapoints(res.Result[0].Values)
			s := aggregate.Summarize(values)
			for _, agg := range aggs {
				row[agg] = pickAgg(s, agg)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func toDatapoints(values [][]any) [][2]any {
	out := make([][2]any, len(values))
	for i, v := range values {
		if len(v) >= 2 {
			out[i] = [2]any{v[0], v[1]}
		}
	}
	return out
}

func pickAgg(s aggregate.Summary, agg string) any {
	switch agg {
	case "count":
		return s.Count
	case "min":
		return s.Min
	case "max":
		return s.Max
	case "avg":
		return s.Avg
	case "p50":
		return s.P50
	case "p95":
		return s.P95
	case "p99":
		return s.P99
	case "last":
		return s.Last
	default:
		return nil
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./cmd/scenarios/... -race -count=1 -v -run TestSummarize
```

Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/scenarios/summarize.go cmd/scenarios/summarize_test.go
git commit -m "feat(scenarios): add summarizeByInstance + summarizeLabelValue

Helpers consume vmclient range Results and project them into output.Row using
aggregate.Summarize. Default aggs are [max,avg,p99,last]; per-query overrides
via scenario.Summary[label].Aggs.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 6: Runner — wire mode routing

**Files:**
- Modify: `cmd/scenarios/runner.go`
- Modify: `cmd/scenarios/runner_test.go` (create if missing — test against fake VM responses)

- [ ] **Step 1: Write failing test**

Create `cmd/scenarios/runner_test.go`:

```go
package scenarios

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

func TestPickMode(t *testing.T) {
	cases := []struct {
		name           string
		scenarioRange  bool
		instantSummary bool
		hasSince       bool
		raw            bool
		want           string
	}{
		{"plain instant", false, false, false, false, "instant"},
		{"range default summary", true, false, true, false, "summary"},
		{"range with --raw", true, false, true, true, "raw"},
		{"hybrid instant scenario without --since", false, true, false, false, "instant"},
		{"hybrid instant scenario with --since", false, true, true, false, "summary"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := promql.Scenario{Range: c.scenarioRange, InstantSummary: c.instantSummary}
			require.Equal(t, c.want, pickMode(s, c.hasSince, c.raw))
		})
	}
}

func TestRun_RangeSummaryEmitsModeAndColumns(t *testing.T) {
	scenario := promql.Scenario{
		Name:    "fake-range",
		Range:   true,
		Columns: []string{"instance", "qps"},
		Queries: []promql.Query{{Label: "qps", Expr: "x"}},
	}
	results := []vmclient.Result{
		{Result: []vmclient.Sample{sample("rs1", "10", "20", "30")}},
	}
	env := buildEnvelope(scenario, []promql.Rendered{{Label: "qps", Expr: "x"}}, results, "summary")
	require.Equal(t, "summary", env.Mode)
	require.Contains(t, env.Columns, "qps_max")
	require.Equal(t, 30.0, env.Data[0]["qps_max"])
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./cmd/scenarios/... -run TestPickMode
go test ./cmd/scenarios/... -run TestRun_RangeSummary
```

Expected: build error — `pickMode`, `buildEnvelope` don't exist.

- [ ] **Step 3: Refactor `runner.go` — add Inputs.Raw, mode routing, helpers**

Replace `internal/runner.go` `Inputs` struct (currently lines 18-27) and `Run` function with:

```go
type Inputs struct {
	Scenario promql.Scenario
	Vars     promql.Vars
	Client   *vmclient.Client
	DryRun   bool
	Raw      bool          // new: --raw forces raw datapoints in range mode
	HasSince bool          // new: --since explicitly provided
	End      time.Time
	Step     time.Duration
	Since    time.Duration
}

// pickMode chooses one of "instant" | "summary" | "raw" given scenario and flags.
func pickMode(s promql.Scenario, hasSince, raw bool) string {
	isRange := s.Range || (s.InstantSummary && hasSince)
	switch {
	case isRange && raw:
		return "raw"
	case isRange:
		return "summary"
	default:
		return "instant"
	}
}

// summaryColumns returns the column list to embed in the summary envelope.
// If scenario.SummaryColumns is set, use it verbatim. Otherwise derive
// `<label>_<agg>` from queries × aggsFor(...).
func summaryColumns(s promql.Scenario) []string {
	if len(s.SummaryColumns) > 0 {
		return s.SummaryColumns
	}
	cols := []string{"instance"}
	for _, q := range s.Queries {
		for _, agg := range aggsFor(s, q.Label) {
			cols = append(cols, q.Label+"_"+agg)
		}
	}
	return cols
}

// labelValueSummaryColumns is used when Columns starts with [label, value]
// — it expands to [label, <aggs of first query>] (all queries share aggs in
// this mode by spec choice; cluster-overview is the only consumer in v0.2).
func labelValueSummaryColumns(s promql.Scenario) []string {
	if len(s.SummaryColumns) > 0 {
		return s.SummaryColumns
	}
	cols := []string{"label"}
	if len(s.Queries) > 0 {
		cols = append(cols, aggsFor(s, s.Queries[0].Label)...)
	}
	return cols
}

// buildEnvelope is the post-fetch projection step, isolated for testability.
func buildEnvelope(s promql.Scenario, rendered []promql.Rendered, results []vmclient.Result, mode string) output.Envelope {
	env := output.Envelope{
		Scenario: s.Name,
		Mode:     mode,
		Queries:  make([]output.Query, len(rendered)),
	}
	for i, r := range rendered {
		env.Queries[i] = output.Query{Label: r.Label, Expr: r.Expr}
	}
	switch mode {
	case "summary":
		if isLabelValueMode(s.Columns) {
			env.Columns = labelValueSummaryColumns(s)
			env.Data = summarizeLabelValue(s, rendered, results)
		} else {
			env.Columns = summaryColumns(s)
			env.Data = summarizeByInstance(s, rendered, results)
		}
	case "raw":
		env.Columns = s.Columns
		if isLabelValueMode(s.Columns) {
			env.Data = aggregateLabelValue(rendered, results, true)
		} else {
			env.Data = mergeByInstance(rendered, results, true)
		}
	default: // instant
		env.Columns = s.Columns
		if isLabelValueMode(s.Columns) {
			env.Data = aggregateLabelValue(rendered, results, false)
		} else {
			env.Data = mergeByInstance(rendered, results, false)
		}
	}
	return env
}

func Run(ctx context.Context, in Inputs) (output.Envelope, error) {
	rendered, err := promql.Render(in.Scenario, in.Vars)
	if err != nil {
		return output.Envelope{}, cerrors.Errorf(cerrors.CodeFlagInvalid, "render scenario %s: %v", in.Scenario.Name, err)
	}
	cluster, _ := in.Vars["cluster"].(string)
	mode := pickMode(in.Scenario, in.HasSince, in.Raw)
	isRange := mode == "summary" || mode == "raw"

	env := buildEnvelope(in.Scenario, rendered, nil, mode)
	env.Cluster = cluster
	if isRange {
		end := in.End
		if end.IsZero() {
			end = time.Now()
		}
		start := end.Add(-in.Since)
		env.Range = &output.Range{
			Start: start.UTC().Format(time.RFC3339),
			End:   end.UTC().Format(time.RFC3339),
			Step:  in.Step.String(),
		}
	}
	if in.DryRun {
		return env, nil
	}

	results := make([]vmclient.Result, len(rendered))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for i, r := range rendered {
		i, r := i, r
		g.Go(func() error {
			var (
				res *vmclient.Result
				err error
			)
			if isRange {
				end := in.End
				if end.IsZero() {
					end = time.Now()
				}
				res, err = in.Client.QueryRange(gctx, r.Expr, end.Add(-in.Since), end, in.Step)
			} else {
				res, err = in.Client.Query(gctx, r.Expr, time.Now())
			}
			if err != nil {
				return err
			}
			results[i] = *res
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return output.Envelope{}, err
	}

	full := buildEnvelope(in.Scenario, rendered, results, mode)
	env.Data = full.Data
	env.Columns = full.Columns
	if len(env.Data) == 0 {
		return env, cerrors.WithHint(
			cerrors.Errorf(cerrors.CodeNoData, "scenario %s returned no data", in.Scenario.Name),
			"verify --cluster matches an active cluster label and --since covers a period with traffic",
		)
	}
	return env, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./cmd/scenarios/... -race -count=1 -v
```

Expected: all tests in this package PASS, including the existing tests after the refactor.

- [ ] **Step 5: Commit**

```bash
git add cmd/scenarios/runner.go cmd/scenarios/runner_test.go
git commit -m "refactor(runner): route between instant/summary/raw modes

pickMode picks the mode from scenario shape + (--since, --raw); buildEnvelope
projects results into the right shape per mode. Default for time-range
scenarios is now \"summary\". Existing instant scenarios are unchanged.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 7: Wire `--raw`, global `--since`, `--step auto`, friendly hint

**Files:**
- Modify: `cmd/root.go`
- Modify: `cmd/scenarios/register.go`

- [ ] **Step 1: Add `--raw` to global flags**

In `cmd/root.go`, extend `globalFlags` (currently lines 20-28):

```go
type globalFlags struct {
	VMURL         string
	Cluster       string
	BasicAuthUser string
	BasicAuthPass string
	Timeout       time.Duration
	Format        string
	DryRun        bool
	Raw           bool
}
```

Add the flag registration in `newRootCmd()` after the existing `BoolVar` for `dry-run`:

```go
root.PersistentFlags().BoolVar(&globals.Raw, "raw", false, "Time-range scenarios: emit raw datapoints instead of aggregated summary")
```

Add the accessor used by scenarios.Register (next change to `register` function in `cmd/root.go`):

```go
func register(root *cobra.Command) {
	root.AddCommand(newVersionCmd())
	if err := scenarios.Register(root, LoadEffectiveConfig,
		func() string { return globals.Format },
		func() bool { return globals.DryRun },
		func() bool { return globals.Raw },
	); err != nil {
		fmt.Fprintf(os.Stderr, "scenario registration failed: %v\n", err)
		os.Exit(cerrors.ExitInternal)
	}
	root.AddCommand(newQueryCmd())
	root.AddCommand(configcmd.New())
}
```

- [ ] **Step 2: Update `Register` signature and add hybrid since handling**

In `cmd/scenarios/register.go`, change the signature and the `buildCmd` body:

Replace `LoadConfigFn`, `FormatFn`, `DryRunFn` block (currently lines 19-26) and add:

```go
type RawFn func() bool
```

Replace `Register` (currently lines 28-37):

```go
func Register(root *cobra.Command, loadCfg LoadConfigFn, format FormatFn, dryRun DryRunFn, raw RawFn) error {
	scenarios, err := promql.LoadEmbedded()
	if err != nil {
		return err
	}
	for _, s := range scenarios {
		root.AddCommand(buildCmd(s, loadCfg, format, dryRun, raw))
	}
	return nil
}
```

Replace `buildCmd` (currently lines 39-136) with this version that:
- registers `--since` on every scenario
- registers `--step` with `auto` default for range / hybrid scenarios
- rejects `--since` on non-hybrid instant scenarios with a friendly hint
- resolves `--step auto` via `stepauto.Resolve(since)`

```go
func buildCmd(s promql.Scenario, loadCfg LoadConfigFn, format FormatFn, dryRun DryRunFn, raw RawFn) *cobra.Command {
	flagValues := map[string]*string{}
	intValues := map[string]*int{}
	since := ""
	step := ""
	rangeCapable := s.Range || s.InstantSummary

	cmd := &cobra.Command{
		Use:   s.Name,
		Short: s.Description,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			vars := promql.Vars{"cluster": cfg.DefaultCluster}
			for k, v := range s.Defaults {
				vars[k] = v
			}
			for name, p := range flagValues {
				if *p != "" {
					vars[name] = *p
				}
			}
			for name, p := range intValues {
				vars[name] = *p
			}

			hasSince := cmd.Flags().Changed("since")
			if hasSince && !rangeCapable {
				return cerrors.WithHint(
					cerrors.Errorf(cerrors.CodeFlagInvalid, "scenario %q is instant; --since does not apply", s.Name),
					"omit --since for instant scenarios; or use a time-range scenario like rpc-latency / gc-pressure",
				)
			}

			// Resolve since / step. Range scenarios still get a default since (5m)
			// when not specified, matching v0.1 behavior.
			effectiveSince := since
			if effectiveSince == "" {
				if s.Range {
					effectiveSince = "5m"
				}
			}
			var sinceDur time.Duration
			if effectiveSince != "" {
				sinceDur, err = time.ParseDuration(effectiveSince)
				if err != nil {
					return cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --since %q: %v", effectiveSince, err)
				}
			}

			var stepDur time.Duration
			if step == "" || step == "auto" {
				stepDur = stepauto.Resolve(sinceDur)
			} else {
				stepDur, err = time.ParseDuration(step)
				if err != nil {
					return cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --step %q: %v", step, err)
				}
			}

			vars["since"] = effectiveSince
			vars["step"] = stepDur.String()
			if sinceDur > 0 {
				vars["since_seconds"] = strconv.Itoa(int(sinceDur.Seconds()))
			}

			client := vmclient.New(vmclient.Options{
				BaseURL:       cfg.VMURL,
				Timeout:       cfg.Timeout,
				BasicAuthUser: cfg.BasicAuth.Username,
				BasicAuthPass: cfg.BasicAuth.Password,
			})

			env, err := Run(cmd.Context(), Inputs{
				Scenario: s,
				Vars:     vars,
				Client:   client,
				DryRun:   dryRun(),
				Raw:      raw(),
				HasSince: hasSince,
				Since:    sinceDur,
				Step:     stepDur,
			})
			if err != nil && !isNoData(err) {
				return err
			}
			if err := output.Render(format(), env, cmd.OutOrStdout()); err != nil {
				return err
			}
			if err != nil { // NoData warning -> stderr, exit 0
				cerrors.WriteJSON(os.Stderr, err)
			}
			return nil
		},
	}

	for _, f := range s.Flags {
		switch f.Type {
		case "int":
			def := 0
			if v, ok := f.Default.(int); ok {
				def = v
			}
			p := new(int)
			*p = def
			cmd.Flags().IntVar(p, f.Name, def, fmt.Sprintf("%s (default %d)", f.Help, def))
			intValues[f.Name] = p
		default:
			def := ""
			if v, ok := f.Default.(string); ok {
				def = v
			}
			p := new(string)
			*p = def
			cmd.Flags().StringVar(p, f.Name, def, f.Help)
			flagValues[f.Name] = p
		}
	}

	// --since is registered on every scenario; instant non-hybrid scenarios
	// reject it at runtime with a hint.
	cmd.Flags().StringVar(&since, "since", "", sinceFlagHelp(s, rangeCapable))
	if rangeCapable {
		cmd.Flags().StringVar(&step, "step", "auto", "Range step (auto | duration like 30s, 5m)")
	}
	cmd.Flags().Bool("range", s.Range, "")
	_ = cmd.Flags().MarkHidden("range")
	return cmd
}

func sinceFlagHelp(s promql.Scenario, rangeCapable bool) string {
	if rangeCapable {
		return "Range duration (e.g. 30m, 24h)"
	}
	return "Not applicable for instant scenarios — omit"
}
```

Add the import for `stepauto` at the top of the file:

```go
import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/stepauto"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)
```

- [ ] **Step 3: Add a regression test for the friendly hint**

Append to `cmd/scenarios/runner_test.go`:

```go
import (
	"bytes"
	"context"

	"github.com/spf13/cobra"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
)

func TestSinceOnInstantScenarioYieldsHint(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	dummyCfg := func() (*config.Config, error) {
		return &config.Config{VMURL: "http://x", DefaultCluster: "c", Timeout: time.Second}, nil
	}
	require.NoError(t, Register(root,
		dummyCfg,
		func() string { return "json" },
		func() bool { return true },
		func() bool { return false },
	))

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"handler-queue", "--since", "1h", "--dry-run"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	var ce *cerrors.CodedError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, cerrors.CodeFlagInvalid, ce.Code)
	require.Contains(t, ce.Hint, "instant scenarios")
}
```

(Note: this test imports `config` — add `"github.com/opay-bigdata/hbase-metrics-cli/internal/config"` to the test file imports.)

- [ ] **Step 4: Run all unit tests**

```bash
make tidy && make lint && make unit-test
```

Expected: all green.

- [ ] **Step 5: Run e2e dry-run**

```bash
make e2e-dry
```

Expected: all 12 scenarios still dry-run successfully.

- [ ] **Step 6: Commit**

```bash
git add cmd/root.go cmd/scenarios/register.go cmd/scenarios/runner_test.go
git commit -m "feat(cmd): --raw, global --since, --step auto, friendly hint

* --raw forces v0.1-shaped raw datapoints in range scenarios.
* --since is registered on every scenario; instant scenarios that did not opt
  into instant_summary return a CodedError with a hint pointing the user
  toward range-capable scenarios.
* --step defaults to \"auto\", resolved via stepauto.Resolve(since).

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 8: Scenario YAML — `cluster-overview` (hybrid + renames + count→max)

**Files:**
- Modify: `scenarios/cluster-overview.yaml`
- Modify: `tests/golden/cluster-overview.golden.promql` (regenerated)

- [ ] **Step 1: Rewrite the scenario YAML**

Replace `scenarios/cluster-overview.yaml` with:

```yaml
name: cluster-overview
description: Cluster-wide HBase health summary (RS count, regions, requests, RPC P99).
range: false
instant_summary: true
columns: [label, value]
summary_columns: [label, max, avg, p99, last]
summary:
  qps_total:
    aggs: [max, avg, p99, last]
  rpc_p99_max_ms:
    aggs: [max, avg, p99, last]
  regionservers_active:
    aggs: [max, last]
  regionservers_dead:
    aggs: [max, last]
  regions_total:
    aggs: [max, last]
queries:
  - label: regionservers_active
    expr: |
      max(hadoop_hbase_numregionservers{cluster="{{.cluster}}", role="master", sub="Server"})
  - label: regionservers_dead
    expr: |
      max(hadoop_hbase_numdeadregionservers{cluster="{{.cluster}}", role="master", sub="Server"})
  - label: regions_total
    expr: |
      sum(hadoop_hbase_regioncount{cluster="{{.cluster}}", role="regionserver", sub="Server"})
  - label: qps_total
    expr: |
      sum(clamp_min(rate(hadoop_hbase_totalrequestcount{cluster="{{.cluster}}", role="regionserver", sub="Server"}[5m]), 0))
  - label: rpc_p99_max_ms
    expr: |
      max(hadoop_hbase_processcalltime_99th_percentile{cluster="{{.cluster}}", role="regionserver", sub="IPC"})
```

- [ ] **Step 2: Regenerate golden**

```bash
go test -run TestRender_Goldens github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
```

Verify the diff in `tests/golden/cluster-overview.golden.promql` only renames labels, swaps `count` for `max`, and adds `clamp_min(... [5m], 0)`.

- [ ] **Step 3: Re-run goldens (without -update) and unit + e2e**

```bash
go test ./tests/golden/... -race -count=1
make e2e-dry
```

Expected: pass; `cluster-overview --dry-run` and `cluster-overview --since 1h --dry-run` both succeed.

- [ ] **Step 4: Commit**

```bash
git add scenarios/cluster-overview.yaml tests/golden/cluster-overview.golden.promql
git commit -m "fix(cluster-overview): hybrid mode + field renames + count→max

* instant_summary: true — accepts --since for windowed summary.
* count(...) replaced with max(...) (was returning 2 with 3 RS online).
* qps total uses clamp_min(rate(..[5m]),0) for counter-reset robustness.
* Field renames: regionserver_count → regionservers_active,
  dead_regionserver_count → regionservers_dead, total_regions → regions_total,
  total_request_qps → qps_total, rpc_processcalltime_p99_max → rpc_p99_max_ms.

BREAKING CHANGE: field names. See CHANGELOG migration table.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 9: Scenario YAML — `requests-qps` (clamp_min + summary spec)

**Files:**
- Modify: `scenarios/requests-qps.yaml`
- Modify: `tests/golden/requests-qps.golden.promql` (regenerated)

- [ ] **Step 1: Rewrite the scenario YAML**

```yaml
name: requests-qps
description: Read / write / total request QPS across the cluster.
range: true
defaults:
  since: 10m
  step: 30s
columns: [instance, read_qps, write_qps, total_qps]
summary:
  read_qps:
    aggs: [max, avg, p99, last]
  write_qps:
    aggs: [max, avg, p99, last]
  total_qps:
    aggs: [max, avg, p99, last]
queries:
  - label: read_qps
    expr: |
      sum by (instance) (clamp_min(rate(hadoop_hbase_readrequestcount{cluster="{{.cluster}}", role="regionserver", sub="Server"}[5m]), 0))
  - label: write_qps
    expr: |
      sum by (instance) (clamp_min(rate(hadoop_hbase_writerequestcount{cluster="{{.cluster}}", role="regionserver", sub="Server"}[5m]), 0))
  - label: total_qps
    expr: |
      sum by (instance) (clamp_min(rate(hadoop_hbase_totalrequestcount{cluster="{{.cluster}}", role="regionserver", sub="Server"}[5m]), 0))
```

- [ ] **Step 2: Regenerate golden + run tests**

```bash
go test -run TestRender_Goldens github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
go test ./tests/golden/... -race -count=1
make e2e-dry
```

- [ ] **Step 3: Commit**

```bash
git add scenarios/requests-qps.yaml tests/golden/requests-qps.golden.promql
git commit -m "fix(requests-qps): clamp_min(rate([5m])) for reset-spike robustness

Counter-reset spikes (region move, RS restart) inflated write_qps_max to
five-figure values. Switching the rate window to 5m and clamping negatives
keeps p99 honest while the user can still see the raw max via --raw.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 10: Scenario YAML — `rpc-latency` (summary spec)

**Files:**
- Modify: `scenarios/rpc-latency.yaml`
- Modify: `tests/golden/rpc-latency.golden.promql`

- [ ] **Step 1: Rewrite YAML**

```yaml
name: rpc-latency
description: HBase RegionServer / Master RPC P99 / P999 latency (ms), top-K instances.
range: true
defaults:
  since: 10m
  step: 30s
flags:
  - name: role
    type: string
    default: regionserver
    enum: [master, regionserver]
    help: Role label value (master | regionserver)
  - name: top
    type: int
    default: 10
    help: Top-K instances by P99
columns: [instance, p99, p999]
summary:
  p99:
    aggs: [max, avg, p99, last]
  p999:
    aggs: [max, avg, last]
queries:
  - label: p99
    expr: |
      topk({{.top}}, hadoop_hbase_processcalltime_99th_percentile{cluster="{{.cluster}}", role="{{.role}}", sub="IPC"})
  - label: p999
    expr: |
      topk({{.top}}, hadoop_hbase_processcalltime_99_9th_percentile{cluster="{{.cluster}}", role="{{.role}}", sub="IPC"})
```

- [ ] **Step 2: Regenerate golden + tests**

```bash
go test -run TestRender_Goldens github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
make e2e-dry
```

- [ ] **Step 3: Commit**

```bash
git add scenarios/rpc-latency.yaml tests/golden/rpc-latency.golden.promql
git commit -m "feat(rpc-latency): summary aggs declared

p99 query reports max/avg/p99/last; p999 reports max/avg/last.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 11: Scenario YAML — `gc-pressure` (summary spec)

**Files:**
- Modify: `scenarios/gc-pressure.yaml`
- Modify: `tests/golden/gc-pressure.golden.promql`

- [ ] **Step 1: Rewrite YAML**

```yaml
name: gc-pressure
description: GC count and elapsed time per RegionServer (per-collector).
range: true
defaults:
  since: 15m
  step: 1m
columns: [instance, gc_count_per_min, gc_time_ms_per_min]
summary:
  gc_count_per_min:
    aggs: [max, avg, last]
  gc_time_ms_per_min:
    aggs: [max, avg, p99, last]
queries:
  - label: gc_count_per_min
    expr: |
      sum by (instance) (rate(jvm_gc_collectioncount{cluster="{{.cluster}}", role="regionserver"}[5m])) * 60
  - label: gc_time_ms_per_min
    expr: |
      sum by (instance) (rate(jvm_gc_collectiontime{cluster="{{.cluster}}", role="regionserver"}[5m])) * 60
```

- [ ] **Step 2: Regenerate golden + tests**

```bash
go test -run TestRender_Goldens github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
make e2e-dry
```

- [ ] **Step 3: Commit**

```bash
git add scenarios/gc-pressure.yaml tests/golden/gc-pressure.golden.promql
git commit -m "feat(gc-pressure): summary aggs declared

gc_time_ms_per_min reports max/avg/p99/last so a single 2.5s pause does not
dominate the headline.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 12: Scenario YAML — `compaction-status` (summary spec)

**Files:**
- Modify: `scenarios/compaction-status.yaml`
- Modify: `tests/golden/compaction-status.golden.promql`

- [ ] **Step 1: Rewrite YAML**

```yaml
name: compaction-status
description: Compaction queue length and minor/major compactions per RegionServer.
range: true
defaults:
  since: 30m
  step: 1m
columns: [instance, compaction_queue, compacted_cells_qps]
summary:
  compaction_queue:
    aggs: [max, avg, last]
  compacted_cells_qps:
    aggs: [max, avg]
queries:
  - label: compaction_queue
    expr: |
      hadoop_hbase_compactionqueuelength{cluster="{{.cluster}}", role="regionserver", sub="Server"}
  - label: compacted_cells_qps
    expr: |
      sum by (instance) (rate(hadoop_hbase_compactedcellscount{cluster="{{.cluster}}", role="regionserver", sub="Server"}[5m]))
```

- [ ] **Step 2: Regenerate + tests**

```bash
go test -run TestRender_Goldens github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
make e2e-dry
```

- [ ] **Step 3: Commit**

```bash
git add scenarios/compaction-status.yaml tests/golden/compaction-status.golden.promql
git commit -m "feat(compaction-status): summary aggs declared

queue: max/avg/last (current depth + worst over window).
compacted_cells_qps: max/avg (no p99 needed — bursty by design).

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 13: Scenario YAML — `wal-stats` (summary spec)

**Files:**
- Modify: `scenarios/wal-stats.yaml`
- Modify: `tests/golden/wal-stats.golden.promql`

- [ ] **Step 1: Rewrite YAML**

```yaml
name: wal-stats
description: WAL append count, sync time P99, and slow append count per RegionServer.
range: true
defaults:
  since: 15m
  step: 30s
columns: [instance, append_qps, sync_p99_ms, slow_appends_per_min]
summary:
  append_qps:
    aggs: [max, avg, last]
  sync_p99_ms:
    aggs: [max, avg, p99, last]
  slow_appends_per_min:
    aggs: [max, avg]
queries:
  - label: append_qps
    expr: |
      sum by (instance) (clamp_min(rate(hadoop_hbase_appendcount{cluster="{{.cluster}}", role="regionserver", sub="WAL"}[5m]), 0))
  - label: sync_p99_ms
    expr: |
      hadoop_hbase_synctime_99th_percentile{cluster="{{.cluster}}", role="regionserver", sub="WAL"}
  - label: slow_appends_per_min
    expr: |
      sum by (instance) (rate(hadoop_hbase_slowappendcount{cluster="{{.cluster}}", role="regionserver", sub="WAL"}[5m])) * 60
```

- [ ] **Step 2: Regenerate + tests**

```bash
go test -run TestRender_Goldens github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
make e2e-dry
```

- [ ] **Step 3: Commit**

```bash
git add scenarios/wal-stats.yaml tests/golden/wal-stats.golden.promql
git commit -m "feat(wal-stats): summary aggs + clamp_min on append_qps

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 14: Scenario YAML — `blockcache-hitrate` (add `read_qps_recent`)

**Files:**
- Modify: `scenarios/blockcache-hitrate.yaml`
- Modify: `tests/golden/blockcache-hitrate.golden.promql`

- [ ] **Step 1: Rewrite YAML**

```yaml
name: blockcache-hitrate
description: BlockCache hit ratio and size per RegionServer (with read volume context).
range: false
columns: [instance, hit_ratio_pct, size_mb, evicted_qps, read_qps_recent]
queries:
  - label: hit_ratio_pct
    expr: |
      hadoop_hbase_blockcacheexpresshitpercent{cluster="{{.cluster}}", role="regionserver", sub="Server"}
  - label: size_mb
    expr: |
      hadoop_hbase_blockcachesize{cluster="{{.cluster}}", role="regionserver", sub="Server"} / 1024 / 1024
  - label: evicted_qps
    expr: |
      sum by (instance) (rate(hadoop_hbase_blockcacheevictioncount{cluster="{{.cluster}}", role="regionserver", sub="Server"}[5m]))
  - label: read_qps_recent
    expr: |
      sum by (instance) (clamp_min(rate(hadoop_hbase_readrequestcount{cluster="{{.cluster}}", role="regionserver", sub="Server"}[5m]), 0))
```

- [ ] **Step 2: Regenerate + tests**

```bash
go test -run TestRender_Goldens github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
make e2e-dry
```

- [ ] **Step 3: Commit**

```bash
git add scenarios/blockcache-hitrate.yaml tests/golden/blockcache-hitrate.golden.promql
git commit -m "feat(blockcache-hitrate): add read_qps_recent for volume context

A 0% hit ratio on a RS with zero reads is misleading. read_qps_recent lets
consumers (and humans) check whether the ratio is statistically meaningful
before alerting.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 15: Scenario YAML — `regionserver-list` (rename fields)

**Files:**
- Modify: `scenarios/regionserver-list.yaml`
- Modify: `tests/golden/regionserver-list.golden.promql`

- [ ] **Step 1: Rewrite YAML**

```yaml
name: regionserver-list
description: Per-RegionServer region count, request QPS, and heap usage.
range: false
columns: [instance, regions_count, qps_total, heap_used_mb]
queries:
  - label: regions_count
    expr: |
      hadoop_hbase_regioncount{cluster="{{.cluster}}", role="regionserver", sub="Server"}
  - label: qps_total
    expr: |
      sum by (instance) (clamp_min(rate(hadoop_hbase_totalrequestcount{cluster="{{.cluster}}", role="regionserver", sub="Server"}[5m]), 0))
  - label: heap_used_mb
    expr: |
      hadoop_hbase_memheapusedm{cluster="{{.cluster}}", role="regionserver"}
```

- [ ] **Step 2: Regenerate + tests**

```bash
go test -run TestRender_Goldens github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
make e2e-dry
```

- [ ] **Step 3: Commit**

```bash
git add scenarios/regionserver-list.yaml tests/golden/regionserver-list.golden.promql
git commit -m "fix(regionserver-list): rename qps→qps_total, regions→regions_count

BREAKING CHANGE: field names. Aligns with cluster-overview vocabulary so
agents do not learn two dialects.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 16: Scenario YAML — sweep no-rename scenarios for clamp_min

**Files:**
- Modify: `scenarios/hotspot-detect.yaml`
- Modify: `tests/golden/hotspot-detect.golden.promql`

(Per spec §5.8, `master-status`, `handler-queue`, `jvm-memory` are gauge-only and have no field renames; `hotspot-detect`'s `qps` query uses `rate(totalrequestcount)` and benefits from the same clamp_min hardening but without renaming.)

- [ ] **Step 1: Rewrite hotspot-detect YAML**

```yaml
name: hotspot-detect
description: Top-K hot RegionServers by request QPS and active-handler ratio.
range: false
flags:
  - name: top
    type: int
    default: 5
    help: Top-K hottest RegionServers
columns: [instance, qps, active_handlers]
queries:
  - label: qps
    expr: |
      topk({{.top}}, sum by (instance) (clamp_min(rate(hadoop_hbase_totalrequestcount{cluster="{{.cluster}}", role="regionserver", sub="Server"}[5m]), 0)))
  - label: active_handlers
    expr: |
      topk({{.top}}, hadoop_hbase_numactivehandler{cluster="{{.cluster}}", role="regionserver", sub="IPC"})
```

- [ ] **Step 2: Regenerate + tests**

```bash
go test -run TestRender_Goldens github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
make e2e-dry
```

- [ ] **Step 3: Commit**

```bash
git add scenarios/hotspot-detect.yaml tests/golden/hotspot-detect.golden.promql
git commit -m "fix(hotspot-detect): clamp_min(rate([5m])) for reset-spike robustness

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 17: Add summary/raw goldens for all range scenarios

**Files:**
- Modify: `tests/golden/golden_test.go`
- Create: `tests/golden/<scenario>.summary.golden.json` (one per range scenario)
- Create: `tests/golden/<scenario>.raw.golden.json` (one per range scenario)

This task creates **deterministic envelope goldens** based on a fake VM result, so we can lock the v0.2 JSON shape for both modes.

- [ ] **Step 1: Extend the golden test with envelope goldens**

Replace `tests/golden/golden_test.go` with a version that — in addition to the existing PromQL goldens — runs `buildEnvelope` against a fixed fake `vmclient.Result` per range scenario:

```go
package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opay-bigdata/hbase-metrics-cli/cmd/scenarios"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestRender_Goldens(t *testing.T) {
	scenarios, err := promql.LoadEmbedded()
	require.NoError(t, err)
	require.NotEmpty(t, scenarios)

	vars := promql.Vars{
		"cluster": "mrs-hbase-oline",
		"role":    "regionserver",
		"top":     5,
		"since":   "10m",
		"step":    "30s",
	}
	for _, s := range scenarios {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			rendered, err := promql.Render(s, vars)
			require.NoError(t, err)
			var b strings.Builder
			for _, r := range rendered {
				b.WriteString("# " + r.Label + "\n" + strings.TrimSpace(r.Expr) + "\n\n")
			}
			path := filepath.Join("..", "golden", s.Name+".golden.promql")
			if *update {
				require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
			} else {
				want, err := os.ReadFile(path)
				require.NoError(t, err, "missing golden %s — run go test -update", path)
				require.Equal(t, string(want), b.String())
			}
		})
	}
}

func TestEnvelope_Goldens(t *testing.T) {
	all, err := promql.LoadEmbedded()
	require.NoError(t, err)
	for _, s := range all {
		s := s
		if !s.Range {
			continue // Hybrid cluster-overview is covered by a separate fixture below.
		}
		t.Run(s.Name+"_summary", func(t *testing.T) {
			env := buildFromFixture(t, s, "summary")
			compareEnvelopeGolden(t, s.Name+".summary.golden.json", env)
		})
		t.Run(s.Name+"_raw", func(t *testing.T) {
			env := buildFromFixture(t, s, "raw")
			compareEnvelopeGolden(t, s.Name+".raw.golden.json", env)
		})
	}
}

// buildFromFixture renders the scenario with the fixed vars then synthesizes
// a deterministic per-query result so the envelope goldens are reproducible.
func buildFromFixture(t *testing.T, s promql.Scenario, mode string) output.Envelope {
	t.Helper()
	vars := promql.Vars{"cluster": "mrs-hbase-oline", "role": "regionserver", "top": 5, "since": "10m", "step": "30s"}
	rendered, err := promql.Render(s, vars)
	require.NoError(t, err)

	results := make([]vmclient.Result, len(rendered))
	for i, r := range rendered {
		results[i] = vmclient.Result{Result: []vmclient.Sample{
			fakeRangeSample("rs-a:19110", r.Label),
			fakeRangeSample("rs-b:19110", r.Label),
		}}
	}
	env := scenarios.BuildEnvelopeForGolden(s, rendered, results, mode)
	env.Cluster = "mrs-hbase-oline"
	return env
}

func fakeRangeSample(instance, label string) vmclient.Sample {
	return vmclient.Sample{
		Metric: map[string]string{"instance": instance},
		Values: [][]any{
			{float64(1_700_000_000), "10"},
			{float64(1_700_000_030), "20"},
			{float64(1_700_000_060), "30"},
		},
	}
}

func compareEnvelopeGolden(t *testing.T, filename string, env output.Envelope) {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(env))
	path := filepath.Join("..", "golden", filename)
	if *update {
		require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden %s — run go test -update", path)
	require.Equal(t, string(want), buf.String())
}
```

- [ ] **Step 2: Expose `buildEnvelope` for the test**

Add a thin re-export in `cmd/scenarios/export_test_support.go` (regular file, exported function — needed because the golden test lives in another package):

```go
package scenarios

import (
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

// BuildEnvelopeForGolden is a stable export used only by tests in
// other packages (e.g. tests/golden) to lock the envelope shape.
func BuildEnvelopeForGolden(s promql.Scenario, rendered []promql.Rendered, results []vmclient.Result, mode string) output.Envelope {
	return buildEnvelope(s, rendered, results, mode)
}
```

- [ ] **Step 3: Generate the golden files**

```bash
go test -run "TestRender_Goldens|TestEnvelope_Goldens" github.com/opay-bigdata/hbase-metrics-cli/tests/golden -args -update
```

Expected: 5 range scenarios × 2 modes = 10 new `.json` files plus 12 updated `.promql` files.

Inspect a sample (`tests/golden/rpc-latency.summary.golden.json`) and confirm `mode = "summary"`, columns include `p99_max`, `p999_max`, etc.

- [ ] **Step 4: Re-run without -update**

```bash
go test ./tests/golden/... -race -count=1 -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add tests/golden/ cmd/scenarios/export_test_support.go
git commit -m "test(golden): lock summary + raw envelope shapes for range scenarios

Each range scenario now has *.summary.golden.json and *.raw.golden.json
covering the mode field, columns, and per-row aggregation keys. Driven
through cmd/scenarios.BuildEnvelopeForGolden.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 18: e2e dry-run — add `cluster-overview --since` variant

**Files:**
- Modify: `tests/e2e/dryrun_test.go`

- [ ] **Step 1: Read current file**

```bash
sed -n '1,80p' tests/e2e/dryrun_test.go
```

- [ ] **Step 2: Add the hybrid invocation case**

In the `allScenarios` slice (or equivalent driver), add an entry that runs `cluster-overview --since 24h --dry-run` and asserts:

- exit code 0
- JSON envelope `mode == "summary"`
- `range` is populated with `step == "5m"` (auto-resolved from 24h)

The exact syntax depends on how the test currently iterates; below is a minimal pattern — adapt to the existing helper:

```go
func TestDryRun_HybridClusterOverview(t *testing.T) {
	out, code := runCLI(t, "cluster-overview", "--since", "24h", "--dry-run", "--format", "json")
	require.Equal(t, 0, code)
	var env map[string]any
	require.NoError(t, json.Unmarshal(out, &env))
	require.Equal(t, "summary", env["mode"])
	rng, _ := env["range"].(map[string]any)
	require.Equal(t, "5m0s", rng["step"])
}
```

- [ ] **Step 3: Run e2e**

```bash
make e2e-dry
```

Expected: all 12 originals + new hybrid case PASS.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/dryrun_test.go
git commit -m "test(e2e): cover cluster-overview --since 24h hybrid mode

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 19: CHANGELOG — record v0.2.0 with rename map

**Files:**
- Modify: `CHANGELOG.md` (create if missing)

- [ ] **Step 1: Read current file**

```bash
test -f CHANGELOG.md && cat CHANGELOG.md || echo "missing — will create"
```

- [ ] **Step 2: Prepend the v0.2.0 entry**

If the file does not exist, create it with the following content. If it exists, prepend the `## v0.2.0 — 2026-04-29` block above the previous top entry.

```markdown
# Changelog

## v0.2.0 — 2026-04-29

### Highlights
- Hybrid mode for instant scenarios: `--since` is now accepted by every scenario. By default, instant scenarios under `--since` return a **summary** (per-instance/per-label aggregates of count/min/max/avg/p50/p95/p99/last) instead of raw datapoints. Pass `--raw` to opt into the original raw point matrix.
- Auto-resolved `--step`: when `--since` is set without `--step`, the CLI picks 30s/1m/2m/5m/10m based on window size.
- Counter-reset hardening: every `rate()` query now uses `clamp_min(rate(...[5m]), 0)` to suppress phantom spikes after restarts.
- Stable schema with the new `mode` envelope field (`instant` | `summary` | `raw`).
- Better error: passing `--since` to a non-hybrid scenario fails with `FLAG_INVALID` and a clear hint.

### Field rename map (BREAKING)

| Scenario | Before | After |
|---|---|---|
| cluster-overview | `regionserver_count` | `regionservers_active` |
| cluster-overview | `dead_regionserver_count` | `regionservers_dead` |
| cluster-overview | `total_regions` | `regions_total` |
| cluster-overview | `total_request_qps` | `qps_total` |
| cluster-overview | `rpc_processcalltime_p99_max` | `rpc_p99_max_ms` |
| regionserver-list | `qps` | `qps_total` |
| regionserver-list | `regions` | `regions_count` |

Downstream agents / dashboards that consumed those JSON keys must update.

### Aggregation semantics
- `regionservers_active` now uses `max_over_time(count(up{...})[5m])` — fixes the case where a scrape miss made the count drop momentarily.
- `qps_total` and `read/write_qps` use `clamp_min(rate(...[5m]), 0)` — fixes phantom 13K/s spikes from counter resets.
- `blockcache-hitrate` adds `read_qps_recent` so the agent can interpret hit-rate=0% at zero reads correctly.

### Output envelope
- Added `mode` field, always one of `instant`, `summary`, `raw`.
- Summary envelope columns: per-instance shape uses `<label>_<agg>` headers; per-label-value shape uses flat `[label, max, avg, p99, last]`.

### Migration
1. Update consumers of the renamed fields (table above).
2. If you previously parsed time-series payloads, switch to the new summary mode (default) or pass `--raw` for the legacy shape.
3. Adjust scripts that hard-coded `--step=30s`; let auto-step pick.

```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): record v0.2.0 hybrid mode + rename map

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 20: CLAUDE.md — document new schema and conventions

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Read the file**

```bash
sed -n '1,200p' CLAUDE.md
```

- [ ] **Step 2: Update the "Scenario YAML schema" section**

Find the section that describes the scenario YAML keys (likely under "Adding a scenario" or "Scenario schema"). Add the new keys with a complete example. Replace any drift between docs and the current schema.

Append/merge the following block into the schema reference:

```markdown
### Scenario YAML schema (v0.2.0)

Required:
- `name` (string)
- `description` (string)
- `range` (bool) — true → range vector, false → instant vector
- `columns` ([]string) — column order in raw mode
- `queries` (list of `{label, expr}`)

Optional (instant scenarios only):
- `instant_summary` (bool) — when true, the scenario accepts `--since` and renders summary mode by default
- `summary_columns` ([]string) — column order in summary mode (e.g. `[label, max, avg, p99, last]` for per-label-value summaries)
- `summary` (map: label → `{aggs: [...]}`) — overrides default `[max, avg, p99, last]` for that query

Mode routing rule (compiled in `cmd/scenarios/runner.go::pickMode`):

```
mode = raw      if --raw
       summary  if scenario.range || (scenario.instant_summary && --since)
       instant  otherwise
```

Counter-reset convention:
- All `rate()` expressions wrap with `clamp_min(rate(...[5m]), 0)` and use a `[5m]` window minimum.
- Use `max_over_time(count(...)[5m])` for "active resource" gauges instead of bare `count()`.
```

Also update the **Output envelope** snippet to include the `mode` field, and the **Exit codes** table (no change — still 0/1/2/3).

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(claude): document hybrid schema, mode routing, counter-reset convention

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 21: README.md — add hybrid examples and link to real-world report

**Files:**
- Modify: `README.md`
- Modify: `README.zh.md`

- [ ] **Step 1: Update the English README**

In `README.md`:

1. Under **Common Flags**, replace the row describing `--format` with a contiguous block that includes:

```markdown
| `--since` | unset | Time window (e.g. `30m`, `2h`, `24h`). Range scenarios always use it; instant scenarios use it only if they declare `instant_summary: true` (currently `cluster-overview`). |
| `--step` | `auto` | PromQL step for range queries. `auto` resolves to 30s / 1m / 2m / 5m / 10m by window size. Use `30s` etc. to override. |
| `--raw` | `false` | For range scenarios under `--since`, return the raw datapoint matrix instead of the per-instance summary. |
```

2. Under **Output Contract**, replace the example envelope with:

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

3. Add a new section **Real-world example** between **Output Contract** and **Configuration**:

```markdown
## Real-world example

A live 24-hour diagnostic walk-through (cluster-overview → rpc-latency → hotspot-detect → gc-pressure → blockcache-hitrate, etc.) is captured at [`docs/examples/2026-04-29-24h-cluster-analysis.md`](docs/examples/2026-04-29-24h-cluster-analysis.md). It demonstrates the recommended diagnostic playbook from the bundled Claude Code skill.
```

- [ ] **Step 2: Mirror into the Chinese README**

Apply the same three changes to `README.zh.md`. Keep wording consistent with the existing Chinese tone (短句、技术术语保留英文).

- [ ] **Step 3: Verify links work**

```bash
test -f docs/examples/2026-04-29-24h-cluster-analysis.md || echo "MISSING — Task 23 must run first"
```

If missing, defer the link verification until Task 23 completes (this is fine — Task 21 and 23 are committed separately).

- [ ] **Step 4: Commit**

```bash
git add README.md README.zh.md
git commit -m "docs(readme): document --since/--step/--raw, add real-world example link

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 22: SKILL.md — refresh playbook with hybrid examples

**Files:**
- Modify: `.claude/skills/hbase-metrics/SKILL.md`

- [ ] **Step 1: Read current SKILL.md**

```bash
sed -n '1,200p' .claude/skills/hbase-metrics/SKILL.md
```

- [ ] **Step 2: Edit the "Twelve scenarios" table to use 24h windows**

Replace the example commands so the agent learns to use `--since 24h` for the cluster-overview lead-in, e.g.

```markdown
| `cluster-overview` | First glance | `hbase-metrics-cli cluster-overview --since 24h --format json` |
| `rpc-latency` | "RPC slow" | `hbase-metrics-cli rpc-latency --top 10 --since 24h` |
| `hotspot-detect` | "One RS hot" | `hbase-metrics-cli hotspot-detect --top 5 --since 24h` |
```

(Keep the rest of the table intact; only change examples that benefit from a wider window.)

- [ ] **Step 3: Add a "Reading summary mode" section**

Insert before "Diagnostic playbook":

```markdown
## Reading summary mode

When `--since` is set, hybrid and range scenarios return `mode: "summary"`. Each row aggregates one instance (or one label value) over the window with `max`, `avg`, `p99`, `last`. Prefer `max` for hotspot detection, `p99` for tail-latency trends, `avg` for sustained load, `last` for the freshest value.

Pass `--raw` only when you need the full datapoint matrix (rare — typically for plotting).
```

- [ ] **Step 4: Update "Common errors" table**

Add the new row:

```markdown
| `FLAG_INVALID` (`--since` rejected) | the scenario is purely instant (no `instant_summary`) — drop the flag or use a hybrid/range scenario |
```

- [ ] **Step 5: Commit**

```bash
git add .claude/skills/hbase-metrics/SKILL.md
git commit -m "docs(skill): teach hybrid --since/--raw and FLAG_INVALID hint

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 23: docs/examples — capture the 24h analysis as a teaching artifact

**Files:**
- Create: `docs/examples/2026-04-29-24h-cluster-analysis.md`

- [ ] **Step 1: Create the directory**

```bash
mkdir -p docs/examples
```

- [ ] **Step 2: Write the example**

Create `docs/examples/2026-04-29-24h-cluster-analysis.md` with the following content. The document is a curated transcript of a live diagnostic — it is meant to teach the playbook, not replace `--help`.

```markdown
# 24-Hour HBase Cluster Health Walk-through

> A live diagnostic captured against `mrs-hbase-oline` on 2026-04-29. Cluster: 3 active RegionServers, ~84 regions, ~1.7K total QPS sustained, no master RIT. The walk-through follows the diagnostic playbook from the bundled Claude Code skill.

## 1. Lead-in — `cluster-overview --since 24h`

```bash
hbase-metrics-cli cluster-overview --since 24h --format json
```

Summary mode returns one row per derived metric. Verdict: 3/3 RS up, 0 dead, regions stable, QPS centered around 1.7K/s with peaks ~4.3K/s (no counter-reset spikes thanks to the `clamp_min(rate(...[5m]), 0)` hardening), RPC P99 max stayed under tens of ms.

## 2. RPC tail latency — `rpc-latency --top 10 --since 24h`

```bash
hbase-metrics-cli rpc-latency --top 10 --since 24h
```

`p99_max` highlights the top offender; `p99_avg` rules out a transient spike. None of the 3 RS broke the 100ms threshold.

## 3. Hotspot — `hotspot-detect --top 5 --since 24h`

```bash
hbase-metrics-cli hotspot-detect --top 5 --since 24h
```

`qps_total_max` reports the per-RS peak. The skew was within 2× — no single-RS hotspot. (If `qps_total_max / qps_total_avg > 5` for one instance, that instance is hot.)

## 4. JVM pressure — `gc-pressure --since 24h` + `jvm-memory`

```bash
hbase-metrics-cli gc-pressure --since 24h
hbase-metrics-cli jvm-memory
```

`gc_pause_p99_max` and `gc_count_rate_max` together gate "is GC the bottleneck". Heap utilization stayed below 80% on every RS.

## 5. Storage layer — `compaction-status --since 24h` + `blockcache-hitrate`

```bash
hbase-metrics-cli compaction-status --since 24h
hbase-metrics-cli blockcache-hitrate
```

Compaction queue depth `max` < 5 across the window — no backlog. BlockCache `hit_ratio` ≈ 0.95 with `read_qps_recent` non-zero — the cache is doing its job. (When `read_qps_recent == 0`, `hit_ratio == 0` is meaningless — read it together.)

## 6. Master & RIT — `master-status`

```bash
hbase-metrics-cli master-status
```

`rit_count == 0`, average load even — the master is healthy.

## Verdict

The cluster is healthy. The walk-through took 6 commands, ~12 seconds wall-clock, and yielded structured JSON the agent can chain into follow-up `query '<promql>'` calls. The `mode`/`columns` envelope keeps every output greppable.
```

- [ ] **Step 3: Commit**

```bash
git add docs/examples/2026-04-29-24h-cluster-analysis.md
git commit -m "docs(examples): add 24h walk-through as a teaching artifact

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 24: Live VM smoke + version tag

**Files:**
- (no source changes — verification + tagging)

- [ ] **Step 1: Confirm config points at the real VM**

```bash
hbase-metrics-cli config show
```

Expected: `vm_url` source `config` (not `default`). If `default`, abort and have the user run `hbase-metrics-cli config init`.

- [ ] **Step 2: Build a local binary**

```bash
make build
ls -lh ./hbase-metrics-cli
```

- [ ] **Step 3: Run the six-command walk-through against live VM**

```bash
./hbase-metrics-cli cluster-overview --since 24h --format json | jq '.mode, .columns'
./hbase-metrics-cli rpc-latency --top 10 --since 24h --format json | jq '.mode, .columns'
./hbase-metrics-cli hotspot-detect --top 5 --since 24h --format json | jq '.mode, .columns'
./hbase-metrics-cli gc-pressure --since 24h --format json | jq '.mode, .columns'
./hbase-metrics-cli compaction-status --since 24h --format json | jq '.mode, .columns'
./hbase-metrics-cli blockcache-hitrate --format json | jq '.mode, .columns, .data[0]'
```

Expected: every command exits 0, `.mode` is `summary` for the first five and `instant` for `blockcache-hitrate`, columns match the schema in the spec.

Also exercise the rejection path:

```bash
./hbase-metrics-cli regionserver-list --since 24h --format json
```

Expected: exit code 2, stderr JSON `{"error":{"code":"FLAG_INVALID","message":"...","hint":"--since is not supported by regionserver-list ..."}}`.

And the `--raw` opt-in:

```bash
./hbase-metrics-cli requests-qps --since 30m --raw --format json | jq '.mode'
```

Expected: `"raw"`.

- [ ] **Step 4: Run the full test suite**

```bash
make unit-test
make e2e-dry
make lint
```

Expected: all green.

- [ ] **Step 5: Tag and push**

```bash
git tag -a v0.2.0 -m "v0.2.0 — hybrid --since, summary mode, counter-reset hardening, schema rename"
git log --oneline v0.1.0..v0.2.0 | head -40
```

(Do **not** `git push --tags` automatically — wait for explicit user instruction.)

- [ ] **Step 6: Final commit (if any drift was found during smoke)**

If smoke surfaces any issue, fix it and commit before tagging. Otherwise this step is a no-op.

---

## Acceptance Criteria

When all 24 tasks are completed, the following must hold (mirrors §12 of the spec):

- [ ] `cluster-overview --since 24h` works and returns `mode: "summary"`.
- [ ] No instant scenario rejects `--since` *unless* it is genuinely incompatible — and when rejected, the error is `FLAG_INVALID` with a precise hint.
- [ ] No phantom counter-reset spike in `requests-qps` / `cluster-overview` `qps_total` over 24h.
- [ ] `regionservers_active` correctly reports 3 (not 2) on the live cluster.
- [ ] `blockcache-hitrate` includes `read_qps_recent`; agent can disambiguate hit-rate=0% at zero reads.
- [ ] All 12 scenarios use the renamed schema; goldens locked.
- [ ] `--step auto` resolves correctly across 30m / 2h / 24h boundaries.
- [ ] All renamed fields documented in CHANGELOG.md.
- [ ] CLAUDE.md, README.md, README.zh.md, SKILL.md all reference the new flags + envelope.
- [ ] `docs/examples/2026-04-29-24h-cluster-analysis.md` exists and reads cleanly.
- [ ] `make unit-test`, `make e2e-dry`, `make lint` all green.
- [ ] Live VM smoke completes without errors.
- [ ] Tag `v0.2.0` created locally.

---

## Self-review checklist

Before handing this plan off, verify:

1. **Spec coverage** — every problem P1–P10 in `docs/superpowers/specs/2026-04-29-hbase-metrics-cli-fixes-design.md` maps to at least one task:
   - P1 (cluster-overview --since rejection) → Task 7, Task 8
   - P2 (1MB raw datapoints) → Tasks 1, 5, 6
   - P3 (RS count = 2) → Task 8
   - P4 (counter-reset spike) → Tasks 9, 16
   - P5 (blockcache 0%) → Task 14
   - P6 (field naming) → Tasks 8, 15, and rename map in CHANGELOG
   - P7 (no --step) → Tasks 2, 7
   - P8 (no worst-instance roll-up) → Tasks 1, 5
   - P9 ("unknown flag" hint) → Task 7
   - P10 (docs drift) → Tasks 19–23

2. **Type / name consistency** — `regionservers_active`, `regionservers_dead`, `regions_total`, `qps_total`, `rpc_p99_max_ms`, `regions_count`, `read_qps_recent` are spelled identically in tasks 8, 14, 15, 17, 19, 20, 21, 23. ✓

3. **Mode routing rule** — appears identically in §5.1 of the spec, Task 6 (`pickMode`), and Task 20 (CLAUDE.md docs). ✓

4. **Default aggs** — `[max, avg, p99, last]` in Task 5 (default), Task 5 column generator, Task 17 goldens, Task 22 SKILL.md. ✓

5. **No placeholders** — search the plan for `TBD`, `TODO`, `to be decided`, `fill in`:

```bash
grep -nE 'TBD|TODO|to be decided|fill in|TODO\(|FIXME' docs/superpowers/plans/2026-04-29-hbase-metrics-cli-v020-fixes.md
```

Expected: zero hits (or only inside committed code blocks where literal `TODO` is required, none expected).

If any check fails, fix inline before declaring the plan ready.
