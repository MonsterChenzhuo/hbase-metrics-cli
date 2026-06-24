// Package cmd wires the cobra command tree.
package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/cmd/configcmd"
	"github.com/opay-bigdata/hbase-metrics-cli/cmd/scenarios"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
)

var version = "dev"

// Globals populated by flag parsing on the root command.
type globalFlags struct {
	VMURL         string
	Cluster       string
	Env           string
	BasicAuthUser string
	BasicAuthPass string
	Timeout       time.Duration
	Format        string
	DryRun        bool
	Raw           bool
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
	root.PersistentFlags().StringVar(&globals.Env, "env", "", "Config profile name from envs: map (overrides HBASE_ENV / active_env)")
	root.PersistentFlags().StringVar(&globals.BasicAuthUser, "basic-auth-user", "", "Basic Auth username (overrides HBASE_VM_USER)")
	root.PersistentFlags().StringVar(&globals.BasicAuthPass, "basic-auth-pass", "", "Basic Auth password (overrides HBASE_VM_PASS)")
	root.PersistentFlags().DurationVar(&globals.Timeout, "timeout", 0, "HTTP timeout (e.g. 10s)")
	root.PersistentFlags().StringVar(&globals.Format, "format", "json", "Output format: json | table | markdown")
	root.PersistentFlags().BoolVar(&globals.DryRun, "dry-run", false, "Print rendered PromQL without calling VictoriaMetrics")
	root.PersistentFlags().BoolVar(&globals.Raw, "raw", false, "Time-range scenarios: emit raw datapoints instead of aggregated summary")
	return root
}

// LoadEffectiveConfig merges flag/env/file/default and validates.
//
// Layer order (lowest → highest precedence):
//
//	default → file flat → env profile overlay → HBASE_* env vars → --vm-url etc. flags
//
// Env profile selection itself is layered:
//
//	cfg.ActiveEnv → HBASE_ENV → --env
func LoadEffectiveConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if name := pickEnvName(cfg); name != "" {
		if err := config.ApplyEnvProfile(cfg, name); err != nil {
			return nil, err
		}
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

// pickEnvName resolves which env profile to activate by precedence:
// --env flag > HBASE_ENV env var > cfg.ActiveEnv (from yaml).
func pickEnvName(cfg *config.Config) string {
	if v := strings.TrimSpace(globals.Env); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("HBASE_ENV")); v != "" {
		return v
	}
	return strings.TrimSpace(cfg.ActiveEnv)
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
	if err := scenarios.Register(root, LoadEffectiveConfig,
		func() string { return globals.Format },
		func() bool { return globals.DryRun },
		func() bool { return globals.Raw },
	); err != nil {
		fmt.Fprintf(os.Stderr, "scenario registration failed: %v\n", err)
		os.Exit(cerrors.ExitInternal)
	}
	root.AddCommand(newQueryCmd())
	root.AddCommand(newClustersCmd())
	root.AddCommand(newMetricsCmd())
	root.AddCommand(newLabelsCmd())
	root.AddCommand(newLabelCheckCmd())
	root.AddCommand(configcmd.New(LoadEffectiveConfig))
}
