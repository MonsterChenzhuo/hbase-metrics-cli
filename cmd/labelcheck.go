package cmd

import (
	"context"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

func newLabelCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "label-check <metric> <label>",
		Short: "Verify whether a label exists on a metric (and list sample values).",
		Long: `Confirms a label is actually emitted by the exporter on the given metric.
PromQL filters silently match nothing when a label is absent, which makes
broken filters look like normal results — this command catches that.

Output: status=present|missing, plus sample values if present and a hint
suggesting alternatives (e.g. role + instance) when missing.

Example:
  hbase-metrics-cli label-check hadoop_hbase_clusterrequests master`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadEffectiveConfig()
			if err != nil {
				return err
			}
			metric, label := args[0], args[1]
			selector := metric
			if cfg.DefaultCluster != "" {
				selector = metric + `{cluster="` + cfg.DefaultCluster + `"}`
			}

			client := vmclient.New(vmclient.Options{
				BaseURL:       cfg.VMURL,
				Timeout:       cfg.Timeout,
				BasicAuthUser: cfg.BasicAuth.Username,
				BasicAuthPass: cfg.BasicAuth.Password,
			})
			ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
			defer cancel()

			series, err := client.Series(ctx, selector, 0)
			if err != nil {
				return err
			}
			if len(series) == 0 {
				return cerrors.WithHint(
					cerrors.Errorf(cerrors.CodeNoData, "no series for %q", metric),
					"verify the metric name and that --cluster matches an active label",
				)
			}

			vals := map[string]struct{}{}
			seriesCount := 0
			for _, s := range series {
				seriesCount++
				if v, ok := s[label]; ok && v != "" {
					vals[v] = struct{}{}
				}
			}

			status := "missing"
			if len(vals) > 0 {
				status = "present"
			}

			sortedVals := make([]string, 0, len(vals))
			for v := range vals {
				sortedVals = append(sortedVals, v)
			}
			sort.Strings(sortedVals)
			sample := sortedVals
			if len(sample) > 10 {
				sample = sample[:10]
			}

			row := output.Row{
				"metric":          metric,
				"label":           label,
				"status":          status,
				"series_total":    seriesCount,
				"distinct_values": len(vals),
				"sample_values":   strings.Join(sample, ","),
			}

			if status == "missing" {
				row["hint"] = "this exporter does not emit `" + label + "` on `" + metric + "`; use `labels " + metric + "` to see what is available (try role/instance/sub)"
			}

			env := output.Envelope{
				Scenario: "label-check",
				Cluster:  cfg.DefaultCluster,
				Queries:  []output.Query{{Label: "series", Expr: selector}},
				Columns:  []string{"metric", "label", "status", "series_total", "distinct_values", "sample_values", "hint"},
				Data:     []output.Row{row},
			}
			return output.Render(globals.Format, env, cmd.OutOrStdout())
		},
	}
}
