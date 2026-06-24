package cmd

import (
	"context"
	"sort"

	"github.com/spf13/cobra"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

// clustersSelector scopes the cluster-label lookup to HBase series so unrelated
// exporters sharing the same VictoriaMetrics don't leak into the list.
func clustersSelector() string {
	return `{__name__=~"hadoop_hbase_.*"}`
}

func newClustersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "List HBase cluster label values available in VictoriaMetrics.",
		Long: `List the distinct cluster= label values present in VictoriaMetrics so
agents (and humans) can discover which HBase clusters this endpoint serves
without guessing. Pass any of these to --cluster (or wire them into envs:
profiles for --env switching). The row matching the configured default_cluster
is flagged with default=true.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadEffectiveConfig()
			if err != nil {
				return err
			}
			client := vmclient.New(vmclient.Options{
				BaseURL:       cfg.VMURL,
				Timeout:       cfg.Timeout,
				BasicAuthUser: cfg.BasicAuth.Username,
				BasicAuthPass: cfg.BasicAuth.Password,
			})
			ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
			defer cancel()

			selector := clustersSelector()
			values, err := client.LabelValues(ctx, "cluster", selector)
			if err != nil {
				return err
			}
			sort.Strings(values)

			env := output.Envelope{
				Scenario: "clusters",
				Cluster:  cfg.DefaultCluster,
				Queries:  []output.Query{{Label: "cluster_values", Expr: selector}},
				Columns:  []string{"cluster", "default"},
			}
			for _, v := range values {
				env.Data = append(env.Data, output.Row{
					"cluster": v,
					"default": v == cfg.DefaultCluster && cfg.DefaultCluster != "",
				})
			}
			if len(env.Data) == 0 {
				return cerrors.WithHint(
					cerrors.Errorf(cerrors.CodeNoData, "no cluster label values found"),
					"confirm the VM endpoint scrapes hadoop_hbase_* metrics, or widen the selector",
				)
			}
			return output.Render(globals.Format, env, cmd.OutOrStdout())
		},
	}
	return cmd
}
