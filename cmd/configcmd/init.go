package configcmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactively create ~/.config/hbase-metrics-cli/config.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r := bufio.NewReader(cmd.InOrStdin())
			w := cmd.OutOrStdout()
			cfg := &config.Config{Timeout: 10 * time.Second}
			cfg.VMURL = prompt(r, w, "VictoriaMetrics URL", "https://vm.example.com/")
			cfg.DefaultCluster = prompt(r, w, "Default cluster label", "")
			cfg.BasicAuth.Username = prompt(r, w, "Basic Auth username (blank to skip)", "")
			if cfg.BasicAuth.Username != "" {
				cfg.BasicAuth.Password = prompt(r, w, "Basic Auth password", "")
			}
			to := prompt(r, w, "HTTP timeout", "10s")
			d, err := time.ParseDuration(to)
			if err != nil {
				return fmt.Errorf("invalid timeout %q: %w", to, err)
			}
			cfg.Timeout = d
			if err := config.Save(cfg); err != nil {
				return err
			}
			path, _ := config.ConfigPath()
			fmt.Fprintf(w, "wrote %s\n", path)
			return nil
		},
	}
}

func prompt(r *bufio.Reader, w io.Writer, label, def string) string {
	fmt.Fprintf(w, "%s [%s]: ", label, def)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}
