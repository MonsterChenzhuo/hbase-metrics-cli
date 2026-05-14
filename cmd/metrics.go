package cmd

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

func metricSelector(prefix, cluster string) string {
	parts := []string{}
	if prefix != "" {
		parts = append(parts, `__name__=~"`+regexp.QuoteMeta(prefix)+`.*"`)
	}
	if cluster != "" {
		parts = append(parts, `cluster="`+cluster+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func newMetricsCmd() *cobra.Command {
	prefix := "hadoop_hbase_"
	all := false
	cmd := &cobra.Command{
		Use:   "metrics [contains]",
		Short: "List metric names available in VictoriaMetrics.",
		Long: `List metric names so agents can discover exporter capabilities without
falling back to raw VictoriaMetrics API calls. By default this is scoped to
HBase metrics (hadoop_hbase_*) and the configured cluster.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadEffectiveConfig()
			if err != nil {
				return err
			}
			effectivePrefix := prefix
			if all {
				effectivePrefix = ""
			}
			contains := ""
			if len(args) > 0 {
				contains = strings.ToLower(args[0])
			}

			selector := metricSelector(effectivePrefix, cfg.DefaultCluster)
			client := vmclient.New(vmclient.Options{
				BaseURL:       cfg.VMURL,
				Timeout:       cfg.Timeout,
				BasicAuthUser: cfg.BasicAuth.Username,
				BasicAuthPass: cfg.BasicAuth.Password,
			})
			ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
			defer cancel()

			names, err := client.MetricNames(ctx, selector)
			if err != nil {
				return err
			}
			sort.Strings(names)
			env := output.Envelope{
				Scenario: "metrics",
				Cluster:  cfg.DefaultCluster,
				Queries:  []output.Query{{Label: "metric_names", Expr: selector}},
				Columns:  []string{"metric"},
			}
			for _, name := range names {
				if contains != "" && !strings.Contains(strings.ToLower(name), contains) {
					continue
				}
				env.Data = append(env.Data, output.Row{"metric": name})
			}
			if len(env.Data) == 0 {
				return cerrors.WithHint(
					cerrors.Errorf(cerrors.CodeNoData, "no metric names matched"),
					"try a broader substring, --prefix hadoop_hbase_, or --all",
				)
			}
			return output.Render(globals.Format, env, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "hadoop_hbase_", "Metric name prefix to request from VictoriaMetrics")
	cmd.Flags().BoolVar(&all, "all", false, "List all metric names instead of only the selected prefix")
	return cmd
}
