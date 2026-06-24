package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// versionString is the single source of truth for both the `version`
// subcommand and the root `--version` flag, so the two never drift.
func versionString() string {
	return fmt.Sprintf("hbase-metrics-cli %s (go %s)", version, runtime.Version())
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build and runtime version info",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), versionString())
			return nil
		},
	}
}
