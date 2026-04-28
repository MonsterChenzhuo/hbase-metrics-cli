// Package cmd wires the cobra command tree.
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/cmd/scenarios"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
)

var version = "dev"

// Globals populated by flag parsing on the root command.
type globalFlags struct {
	VMURL          string
	Cluster        string
	BasicAuthUser  string
	BasicAuthPass  string
	Timeout        time.Duration
	Format         string
	DryRun         bool
}

var globals globalFlags

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "hbase-metrics-cli",
		Short: "Diagnose HBase clusters via VictoriaMetrics — designed for Claude Code.",
		Long: `hbase-metrics-cli runs predefined diagnostic scenarios against a
VictoriaMetrics endpoint and emits structured JSON / table / markdown so
Claude Code (and other AI agents) can analyze HBase health.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&globals.VMURL, "vm-url", "", "VictoriaMetrics base URL (overrides config / HBASE_VM_URL)")
	root.PersistentFlags().StringVar(&globals.Cluster, "cluster", "", "Cluster label value (overrides default_cluster)")
	root.PersistentFlags().StringVar(&globals.BasicAuthUser, "basic-auth-user", "", "Basic Auth username (overrides HBASE_VM_USER)")
	root.PersistentFlags().StringVar(&globals.BasicAuthPass, "basic-auth-pass", "", "Basic Auth password (overrides HBASE_VM_PASS)")
	root.PersistentFlags().DurationVar(&globals.Timeout, "timeout", 0, "HTTP timeout (e.g. 10s)")
	root.PersistentFlags().StringVar(&globals.Format, "format", "json", "Output format: json | table | markdown")
	root.PersistentFlags().BoolVar(&globals.DryRun, "dry-run", false, "Print rendered PromQL without calling VictoriaMetrics")
	return root
}

// LoadEffectiveConfig merges flag/env/file/default and validates.
func LoadEffectiveConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	config.ApplyEnv(cfg)
	config.ApplyFlags(cfg, config.FlagOverrides{
		VMURL:          globals.VMURL,
		DefaultCluster: globals.Cluster,
		BasicAuthUser:  globals.BasicAuthUser,
		BasicAuthPass:  globals.BasicAuthPass,
		Timeout:        globals.Timeout,
	})
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Execute is the package entry point invoked by main.go.
func Execute() int {
	root := newRootCmd()
	register(root) // wires version, scenarios, query, config
	if err := root.Execute(); err != nil {
		cerrors.WriteJSON(os.Stderr, err)
		return cerrors.ExitCode(err)
	}
	return cerrors.ExitOK
}

// register is implemented in init.go to keep newRootCmd minimal.
func register(root *cobra.Command) {
	root.AddCommand(newVersionCmd())
	if err := scenarios.Register(root, LoadEffectiveConfig, func() string { return globals.Format }, func() bool { return globals.DryRun }); err != nil {
		fmt.Fprintf(os.Stderr, "scenario registration failed: %v\n", err)
		os.Exit(cerrors.ExitInternal)
	}
	// query.Register(root) added in Task 9
	// configcmd.Register(root) added in Task 9
}
