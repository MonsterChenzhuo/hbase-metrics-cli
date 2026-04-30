package scenarios

import (
	"sort"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/aggregate"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

var defaultAggs = []string{"max", "avg", "p99", "last"}

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
	sort.Slice(out, func(i, j int) bool {
		ai, _ := out[i]["instance"].(string)
		aj, _ := out[j]["instance"].(string)
		return ai < aj
	})
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

// pickAgg returns the requested aggregate from a Summary. When the summary
// has zero valid datapoints (Count == 0, or every datapoint was NaN/Inf), all
// numeric aggregates collapse to nil so JSON consumers can distinguish "no
// data in window" from a genuine zero reading. `count` and `nan_ratio` stay
// numeric because they describe coverage, not the metric.
func pickAgg(s aggregate.Summary, agg string) any {
	if agg == "count" {
		return s.Count
	}
	if agg == "nan_ratio" {
		return s.NaNRatio
	}
	if !hasValidData(s) {
		return nil
	}
	switch agg {
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

func hasValidData(s aggregate.Summary) bool {
	if s.Count == 0 {
		return false
	}
	return s.NaNRatio < 1.0
}
