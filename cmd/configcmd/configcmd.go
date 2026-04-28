// Package configcmd hosts the `config` subcommands.
package configcmd

import "github.com/spf13/cobra"

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage hbase-metrics-cli configuration",
	}
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newShowCmd())
	return cmd
}
