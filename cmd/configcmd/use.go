package configcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
)

// newUseCmd persists the active environment profile so subsequent commands
// default to it without repeating --env on every invocation. Before this,
// switching environments meant either editing the YAML by hand or passing
// --env / HBASE_ENV on every single command — a friction point hit the first
// time an operator tried `config use <env>` (which silently didn't exist).
func newUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <env>",
		Short: "Set the active environment profile (persists active_env in config)",
		Long: "Persist active_env in the config file so future commands default " +
			"to this profile. Runtime --env / HBASE_ENV still override it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Load the on-disk config (file + defaults) so we preserve envs and
			// flat fields when writing back.
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			known := config.EnvNames(cfg)
			if _, ok := cfg.Envs[name]; !ok {
				hint := "no env profiles are defined; add an `envs:` map to your config first"
				if len(known) > 0 {
					hint = "known profiles: " + strings.Join(known, ", ")
				}
				return cerrors.WithHint(
					cerrors.Errorf(cerrors.CodeConfigInvalid, "unknown env profile %q", name),
					hint,
				)
			}

			cfg.ActiveEnv = name
			if err := config.Save(cfg); err != nil {
				return cerrors.Errorf(cerrors.CodeConfigInvalid, "save config: %v", err)
			}

			path, _ := config.ConfigPath()
			fmt.Fprintf(cmd.OutOrStdout(), "active_env set to %q in %s\n", name, path)
			return nil
		},
	}
}
