// Package scenarios runs predefined HBase metric scenarios against VictoriaMetrics.
package scenarios

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

type Inputs struct {
	Scenario promql.Scenario
	Vars     promql.Vars
	Client   *vmclient.Client
	DryRun   bool
	// Range params (only used when Scenario.Range is true)
	End   time.Time
	Step  time.Duration
	Since time.Duration
}

func Run(ctx context.Context, in Inputs) (output.Envelope, error) {
	rendered, err := promql.Render(in.Scenario, in.Vars)
	if err != nil {
		return output.Envelope{}, cerrors.Errorf(cerrors.CodeFlagInvalid, "render scenario %s: %v", in.Scenario.Name, err)
	}
	cluster, _ := in.Vars["cluster"].(string)
	env := output.Envelope{
		Scenario: in.Scenario.Name,
		Cluster:  cluster,
		Queries:  make([]output.Query, len(rendered)),
		Columns:  in.Scenario.Columns,
	}
	for i, r := range rendered {
		env.Queries[i] = output.Query{Label: r.Label, Expr: r.Expr}
	}
	if in.Scenario.Range {
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
			if in.Scenario.Range {
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

	if isLabelValueMode(in.Scenario.Columns) {
		env.Data = aggregateLabelValue(rendered, results, in.Scenario.Range)
	} else {
		env.Data = mergeByInstance(rendered, results, in.Scenario.Range)
	}
	if len(env.Data) == 0 {
		return env, cerrors.WithHint(
			cerrors.Errorf(cerrors.CodeNoData, "scenario %s returned no data", in.Scenario.Name),
			"verify --cluster matches an active cluster label and --since covers a period with traffic",
		)
	}
	return env, nil
}

// isLabelValueMode reports whether the scenario projects scalar query results
// into a two-column [label, value] table (one row per query).
func isLabelValueMode(cols []string) bool {
	return len(cols) == 2 && cols[0] == "label" && cols[1] == "value"
}

// aggregateLabelValue produces one row per query: {label: query_label, value: scalar}.
// Used for cluster-wide summaries where each query returns a single aggregated number
// without an instance label.
func aggregateLabelValue(rendered []promql.Rendered, results []vmclient.Result, isRange bool) []output.Row {
	rows := make([]output.Row, 0, len(rendered))
	for i, res := range results {
		row := output.Row{"label": rendered[i].Label, "value": nil}
		if len(res.Result) > 0 {
			sample := res.Result[0]
			if isRange {
				row["value"] = sample.Values
			} else {
				row["value"] = parseFloat(sample.Value)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// mergeByInstance turns N parallel query results into one row per instance,
// with the query Label as the column key.
func mergeByInstance(rendered []promql.Rendered, results []vmclient.Result, isRange bool) []output.Row {
	rows := map[string]output.Row{}
	for i, res := range results {
		label := rendered[i].Label
		for _, sample := range res.Result {
			key := sample.Metric["instance"]
			if key == "" {
				key = fmt.Sprintf("%s/%d", label, i)
			}
			row, ok := rows[key]
			if !ok {
				row = output.Row{"instance": key}
				rows[key] = row
			}
			if isRange {
				row[label] = sample.Values
			} else {
				row[label] = parseFloat(sample.Value)
			}
		}
	}
	out := make([]output.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}
	return out
}

func parseFloat(v []any) any {
	if len(v) < 2 {
		return nil
	}
	if s, ok := v[1].(string); ok {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return v[1]
}
