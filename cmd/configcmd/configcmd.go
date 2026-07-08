// Package configcmd hosts the `config` subcommands.
package configcmd

import (
	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
)

// LoadEffectiveFn returns the fully-merged config (file + env profile +
// env vars + flags). Injected from cmd to avoid an import cycle so that
// `config show` honors --env / --vm-url / HBASE_ENV identically to every
// other subcommand.
type LoadEffectiveFn func() (*config.Config, error)

func New(loadEffective LoadEffectiveFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage hbase-metrics-cli configuration",
	}
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newShowCmd(loadEffective))
	cmd.AddCommand(newUseCmd())
	return cmd
}
