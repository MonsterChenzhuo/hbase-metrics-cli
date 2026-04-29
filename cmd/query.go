package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

// queryHasClusterFilter is a coarse heuristic — true when the raw PromQL
// references a `cluster=` label match. Good enough to nudge users; not a
// PromQL parser. Exposed (unexported but testable in cmd package) so the
// warning logic can be unit-tested.
func queryHasClusterFilter(expr string) bool {
	return strings.Contains(expr, "cluster=") || strings.Contains(expr, "cluster!=") ||
		strings.Contains(expr, "cluster=~") || strings.Contains(expr, "cluster!~")
}

func newQueryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query <promql>",
		Short: "Run a raw PromQL instant query (escape hatch).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadEffectiveConfig()
			if err != nil {
				return err
			}
			if !queryHasClusterFilter(args[0]) && cfg.DefaultCluster != "" {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: query has no cluster filter; results may span all clusters. Add `cluster=%q` to scope it.\n",
					cfg.DefaultCluster)
			}
			client := vmclient.New(vmclient.Options{
				BaseURL:       cfg.VMURL,
				Timeout:       cfg.Timeout,
				BasicAuthUser: cfg.BasicAuth.Username,
				BasicAuthPass: cfg.BasicAuth.Password,
			})
			ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
			defer cancel()
			res, err := client.Query(ctx, args[0], time.Now())
			if err != nil {
				return err
			}
			env := output.Envelope{
				Scenario: "query",
				Cluster:  cfg.DefaultCluster,
				Queries:  []output.Query{{Label: "raw", Expr: args[0]}},
				Data:     []output.Row{},
			}
			for _, s := range res.Result {
				row := output.Row{"instance": s.Metric["instance"]}
				if len(s.Value) >= 2 {
					row["value"] = s.Value[1]
				}
				for k, v := range s.Metric {
					if k != "instance" {
						row[k] = v
					}
				}
				env.Data = append(env.Data, row)
			}
			env.Columns = []string{"instance", "value"}
			if len(env.Data) == 0 {
				return cerrors.WithHint(cerrors.Errorf(cerrors.CodeNoData, "query returned no data"), "verify the PromQL expression and label values")
			}
			return output.Render(globals.Format, env, cmd.OutOrStdout())
		},
	}
}
