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
