package scenarios

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/aggregate"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

func sortedKeys(row output.Row) []string {
	out := make([]string, 0, len(row))
	for k := range row {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

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
		Name:    "cluster-overview",
		Columns: []string{"label", "value"},
		Queries: []promql.Query{{Label: "qps_total"}, {Label: "regions_total"}},
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

func TestPickAgg_NoValidData_ReturnsNil(t *testing.T) {
	empty := aggregate.Summary{}
	require.Nil(t, pickAgg(empty, "max"))
	require.Nil(t, pickAgg(empty, "avg"))
	require.Nil(t, pickAgg(empty, "p99"))
	require.Nil(t, pickAgg(empty, "last"))
	require.Equal(t, 0, pickAgg(empty, "count"))

	allInvalid := aggregate.Summary{Count: 5, NaNRatio: 1.0}
	require.Nil(t, pickAgg(allInvalid, "max"))
	require.Nil(t, pickAgg(allInvalid, "last"))
	require.Equal(t, 5, pickAgg(allInvalid, "count"))
	require.Equal(t, 1.0, pickAgg(allInvalid, "nan_ratio"))
}

func TestPickAgg_ValidData_ReturnsNumeric(t *testing.T) {
	s := aggregate.Summary{Count: 3, Max: 30, Avg: 20, P99: 30, Last: 30}
	require.Equal(t, 30.0, pickAgg(s, "max"))
	require.Equal(t, 20.0, pickAgg(s, "avg"))
	require.Equal(t, 30.0, pickAgg(s, "p99"))
	require.Equal(t, 30.0, pickAgg(s, "last"))
}

// TestEnvelopeSchemaInvariant_AllScenarios is the schema watchdog: for every
// embedded scenario, in every mode, every row in env.Data must have exactly
// the keys declared in env.Columns. This caught a master-status bug where an
// agg ran for one query but was absent from summary_columns, leaking an extra
// `avg` field that violated the documented Columns contract.
func TestEnvelopeSchemaInvariant_AllScenarios(t *testing.T) {
	all, err := promql.LoadEmbedded()
	require.NoError(t, err)
	require.NotEmpty(t, all)

	for _, s := range all {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			vars := promql.Vars{
				"cluster": "c", "role": "regionserver", "top": 5,
				"since": "10m", "step": "30s",
			}
			for _, mode := range []string{"instant", "summary", "raw"} {
				vars["mode"] = mode
				vars["is_summary"] = mode != "instant"
				rendered, err := promql.Render(s, vars)
				require.NoError(t, err)

				results := make([]vmclient.Result, len(rendered))
				for i, r := range rendered {
					results[i] = vmclient.Result{Result: []vmclient.Sample{
						sample("rs-a:19110", "10", "20", "30"),
						sample("rs-b:19110", "1", "2", "3"),
					}}
					_ = r
				}

				env := buildEnvelope(s, rendered, results, mode)
				cols := map[string]struct{}{}
				for _, c := range env.Columns {
					cols[c] = struct{}{}
				}
				for i, row := range env.Data {
					for k := range row {
						_, ok := cols[k]
						require.Truef(t, ok,
							"scenario=%s mode=%s row[%d] has key %q not in columns %v",
							s.Name, mode, i, k, env.Columns)
					}
					for c := range cols {
						_, ok := row[c]
						require.Truef(t, ok,
							"scenario=%s mode=%s row[%d] missing column %q (have %v)",
							s.Name, mode, i, c, sortedKeys(row))
					}
				}
			}
		})
	}
}
