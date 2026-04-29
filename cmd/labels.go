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

func newLabelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "labels <metric>",
		Short: "List label keys (and value cardinality) for a metric.",
		Long: `Discover the schema of a metric: which labels it carries and how many
distinct values each label has. Useful when writing PromQL queries against
an unfamiliar exporter.

Example:
  hbase-metrics-cli labels hadoop_hbase_clusterrequests`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadEffectiveConfig()
			if err != nil {
				return err
			}
			metric := args[0]
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

			// label -> set of values
			values := map[string]map[string]struct{}{}
			for _, s := range series {
				for k, v := range s {
					if k == "__name__" {
						continue
					}
					if values[k] == nil {
						values[k] = map[string]struct{}{}
					}
					values[k][v] = struct{}{}
				}
			}

			labelNames := make([]string, 0, len(values))
			for k := range values {
				labelNames = append(labelNames, k)
			}
			sort.Strings(labelNames)

			env := output.Envelope{
				Scenario: "labels",
				Cluster:  cfg.DefaultCluster,
				Queries:  []output.Query{{Label: "series", Expr: selector}},
				Columns:  []string{"label", "cardinality", "sample_values"},
			}
			for _, name := range labelNames {
				vs := make([]string, 0, len(values[name]))
				for v := range values[name] {
					vs = append(vs, v)
				}
				sort.Strings(vs)
				sample := vs
				if len(sample) > 5 {
					sample = sample[:5]
				}
				env.Data = append(env.Data, output.Row{
					"label":         name,
					"cardinality":   len(vs),
					"sample_values": strings.Join(sample, ","),
				})
			}
			return output.Render(globals.Format, env, cmd.OutOrStdout())
		},
	}
}
