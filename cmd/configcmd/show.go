package configcmd

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print effective configuration with sources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			config.ApplyEnv(cfg)
			path, _ := config.ConfigPath()
			out := map[string]any{
				"path":            path,
				"vm_url":          cfg.VMURL,
				"default_cluster": cfg.DefaultCluster,
				"basic_auth_set":  cfg.BasicAuth.Username != "" || cfg.BasicAuth.Password != "",
				"timeout":         cfg.Timeout.String(),
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
