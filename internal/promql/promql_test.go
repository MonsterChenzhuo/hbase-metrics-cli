package promql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_FiltersUnderscoreFiles(t *testing.T) {
	scenarios, err := LoadEmbedded()
	require.NoError(t, err)
	for _, s := range scenarios {
		require.NotEqual(t, '_', s.Name[0])
	}
}

func TestRender_SubstitutesClusterAndRole(t *testing.T) {
	s := Scenario{
		Name: "demo",
		Queries: []Query{
			{Label: "p99", Expr: `topk({{.top}}, my_metric{cluster="{{.cluster}}", role="{{.role}}"})`},
		},
		Defaults: map[string]any{"top": 10},
	}
	rendered, err := Render(s, Vars{"cluster": "c1", "role": "regionserver"})
	require.NoError(t, err)
	require.Len(t, rendered, 1)
	require.Equal(t, "p99", rendered[0].Label)
	require.Contains(t, rendered[0].Expr, `cluster="c1"`)
	require.Contains(t, rendered[0].Expr, `role="regionserver"`)
	require.Contains(t, rendered[0].Expr, "topk(10,")
}

func TestRender_UnknownTemplateVarFails(t *testing.T) {
	s := Scenario{
		Name:    "demo",
		Queries: []Query{{Label: "x", Expr: `{{.missing}}`}},
	}
	_, err := Render(s, Vars{})
	require.Error(t, err)
}

func TestParseScenario_ValidatesRequiredFields(t *testing.T) {
	_, err := ParseScenario([]byte(`name: ""`))
	require.Error(t, err)
}

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
