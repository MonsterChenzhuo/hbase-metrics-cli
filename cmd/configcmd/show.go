package configcmd

import (
	"encoding/json"
	"time"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
)

func newShowCmd(loadEffective LoadEffectiveFn) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print effective configuration with sources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadEffective()
			if err != nil {
				return err
			}
			path, _ := config.ConfigPath()

			envs := map[string]any{}
			for _, name := range config.EnvNames(cfg) {
				ev := cfg.Envs[name]
				envs[name] = map[string]any{
					"vm_url":          ev.VMURL,
					"default_cluster": ev.DefaultCluster,
					"basic_auth_set":  ev.BasicAuth.Username != "" || ev.BasicAuth.Password != "",
					"timeout":         envTimeoutString(ev.Timeout),
				}
			}

			out := map[string]any{
				"path":            path,
				"vm_url":          cfg.VMURL,
				"default_cluster": cfg.DefaultCluster,
				"basic_auth_set":  cfg.BasicAuth.Username != "" || cfg.BasicAuth.Password != "",
				"timeout":         cfg.Timeout.String(),
				"active_env":      cfg.ActiveEnv,
				"selected_env":    cfg.SelectedEnv,
				"envs":            envs,
				"sources": map[string]string{
					"vm_url":          string(cfg.Source.VMURL),
					"default_cluster": string(cfg.Source.DefaultCluster),
					"basic_auth":      string(cfg.Source.BasicAuth),
					"timeout":         string(cfg.Source.Timeout),
				},
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
}

func envTimeoutString(d time.Duration) string {
	if d == 0 {
		return ""
	}
	return d.String()
}
