package scenarios

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

func TestRun_DryRunSkipsHTTPAndEmitsExprs(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()

	s := promql.Scenario{
		Name:    "demo",
		Range:   false,
		Queries: []promql.Query{{Label: "x", Expr: `up{cluster="{{.cluster}}"}`}},
		Columns: []string{"label", "value"},
	}
	out, err := Run(context.Background(), Inputs{
		Scenario: s,
		Vars:     promql.Vars{"cluster": "c1"},
		Client:   vmclient.New(vmclient.Options{BaseURL: srv.URL, Timeout: time.Second}),
		DryRun:   true,
	})
	require.NoError(t, err)
	require.Equal(t, 0, hits)
	require.Equal(t, `up{cluster="c1"}`, out.Queries[0].Expr)
	require.Empty(t, out.Data)
}

func TestRun_InstantQueryPopulatesData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"a"},"value":[1714291200,"1.5"]}]}}`))
	}))
	defer srv.Close()

	s := promql.Scenario{
		Name:    "demo",
		Range:   false,
		Queries: []promql.Query{{Label: "x", Expr: `up`}},
	}
	out, err := Run(context.Background(), Inputs{
		Scenario: s,
		Client:   vmclient.New(vmclient.Options{BaseURL: srv.URL, Timeout: time.Second}),
	})
	require.NoError(t, err)
	require.Len(t, out.Data, 1)
	require.Equal(t, "a", out.Data[0]["instance"])
	require.InDelta(t, 1.5, out.Data[0]["x"], 1e-9)
}

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

// Cluster-overview-style scenario: explicit summary_columns with [max, avg,
// p99, last] but per-query aggs subset [max, last]. Every row must contain
// every declared column; absent aggs are explicit nil so the JSON shape stays
// stable for AI agents.
func TestBuildEnvelope_FillsDeclaredSummaryColumnsWithNil(t *testing.T) {
	scenario := promql.Scenario{
		Name:           "fake-overview",
		Range:          false,
		InstantSummary: true,
		Columns:        []string{"label", "value"},
		SummaryColumns: []string{"label", "max", "avg", "p99", "last"},
		Summary: map[string]promql.SummarySpec{
			"regionservers_active": {Aggs: []string{"max", "last"}},
		},
		Queries: []promql.Query{{Label: "regionservers_active", Expr: "x"}},
	}
	results := []vmclient.Result{{Result: []vmclient.Sample{{
		Metric: map[string]string{},
		Values: [][]any{
			{float64(1_700_000_000), "3"},
			{float64(1_700_000_030), "3"},
			{float64(1_700_000_060), "3"},
		},
	}}}}

	env := buildEnvelope(scenario, []promql.Rendered{{Label: "regionservers_active", Expr: "x"}}, results, "summary")
	require.Equal(t, []string{"label", "max", "avg", "p99", "last"}, env.Columns)
	require.Len(t, env.Data, 1)
	row := env.Data[0]
	require.Equal(t, "regionservers_active", row["label"])
	require.Equal(t, 3.0, row["max"])
	require.Equal(t, 3.0, row["last"])
	require.Contains(t, row, "avg")
	require.Nil(t, row["avg"])
	require.Contains(t, row, "p99")
	require.Nil(t, row["p99"])
}

// When a query returns no series, the label-value summary row must still
// carry every declared column with explicit nil, not an "empty except label"
// row.
func TestBuildEnvelope_LabelValueNoData_AggsAreNil(t *testing.T) {
	scenario := promql.Scenario{
		Name:           "fake-overview",
		Range:          false,
		InstantSummary: true,
		Columns:        []string{"label", "value"},
		SummaryColumns: []string{"label", "max", "avg", "last"},
		Queries:        []promql.Query{{Label: "qps", Expr: "x"}},
	}
	results := []vmclient.Result{{Result: nil}}

	env := buildEnvelope(scenario, []promql.Rendered{{Label: "qps", Expr: "x"}}, results, "summary")
	require.Len(t, env.Data, 1)
	row := env.Data[0]
	require.Equal(t, "qps", row["label"])
	require.Contains(t, row, "max")
	require.Nil(t, row["max"])
	require.Nil(t, row["avg"])
	require.Nil(t, row["last"])
}

// Per-instance summary row: when every datapoint is NaN/Inf, agg fields
// collapse to nil rather than the misleading 0.0 zero-value.
func TestBuildEnvelope_PerInstanceAllNaN_AggsAreNil(t *testing.T) {
	scenario := promql.Scenario{
		Name:    "fake-range",
		Range:   true,
		Columns: []string{"instance", "qps"},
		Queries: []promql.Query{{Label: "qps", Expr: "x"}},
	}
	results := []vmclient.Result{
		{Result: []vmclient.Sample{sample("rs1", "NaN", "+Inf", "NaN")}},
	}
	env := buildEnvelope(scenario, []promql.Rendered{{Label: "qps", Expr: "x"}}, results, "summary")
	require.Len(t, env.Data, 1)
	row := env.Data[0]
	require.Equal(t, "rs1", row["instance"])
	require.Nil(t, row["qps_max"])
	require.Nil(t, row["qps_avg"])
	require.Nil(t, row["qps_last"])
}

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
	root.SetArgs([]string{"handler-queue", "--since", "1h"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	var ce *cerrors.CodedError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, cerrors.CodeFlagInvalid, ce.Code)
	require.Contains(t, ce.Hint, "instant scenarios")
}
