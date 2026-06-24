package cmd

import (
	"context"
	"fmt"
	"sort"
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

// queryColumns builds the column header for the query envelope: the fixed
// instance/value pair followed by every other label seen across the result,
// sorted alphabetically for deterministic table/markdown output. Without this
// the table and markdown renderers would drop labels like `cluster`.
func queryColumns(labelSet map[string]struct{}) []string {
	extra := make([]string, 0, len(labelSet))
	for k := range labelSet {
		extra = append(extra, k)
	}
	sort.Strings(extra)
	return append([]string{"instance", "value"}, extra...)
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
			// Collect every label the result carries so table/markdown
			// rendering doesn't silently drop columns (e.g. `cluster`).
			// JSON already round-trips the full Row map, but the table
			// and markdown renderers only emit keys listed in Columns.
			labelSet := map[string]struct{}{}
			for _, s := range res.Result {
				row := output.Row{"instance": s.Metric["instance"]}
				if len(s.Value) >= 2 {
					row["value"] = s.Value[1]
				}
				for k, v := range s.Metric {
					if k != "instance" {
						row[k] = v
						labelSet[k] = struct{}{}
					}
				}
				env.Data = append(env.Data, row)
			}
			if len(env.Data) == 0 {
				return cerrors.WithHint(cerrors.Errorf(cerrors.CodeNoData, "query returned no data"), "verify the PromQL expression and label values")
			}
			// Column order: instance, value, then remaining labels alphabetically
			// for deterministic output. Every row is back-filled below so the
			// header is a stable schema even when a series lacks a label.
			env.Columns = queryColumns(labelSet)
			for _, row := range env.Data {
				for _, c := range env.Columns {
					if _, ok := row[c]; !ok {
						row[c] = nil
					}
				}
			}
			return output.Render(globals.Format, env, cmd.OutOrStdout())
		},
	}
}
