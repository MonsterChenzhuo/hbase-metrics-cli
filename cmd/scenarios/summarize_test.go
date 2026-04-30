package scenarios

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/aggregate"
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
