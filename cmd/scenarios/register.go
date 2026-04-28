package scenarios

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

// LoadConfigFn is wired by the cmd package to break an import cycle.
type LoadConfigFn func() (*config.Config, error)

// FormatFn returns the global --format value.
type FormatFn func() string

// DryRunFn returns the global --dry-run flag.
type DryRunFn func() bool

func Register(root *cobra.Command, loadCfg LoadConfigFn, format FormatFn, dryRun DryRunFn) error {
	scenarios, err := promql.LoadEmbedded()
	if err != nil {
		return err
	}
	for _, s := range scenarios {
		root.AddCommand(buildCmd(s, loadCfg, format, dryRun))
	}
	return nil
}

func buildCmd(s promql.Scenario, loadCfg LoadConfigFn, format FormatFn, dryRun DryRunFn) *cobra.Command {
	flagValues := map[string]*string{}
	intValues := map[string]*int{}
	since := "5m"
	step := "30s"

	cmd := &cobra.Command{
		Use:   s.Name,
		Short: s.Description,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			vars := promql.Vars{"cluster": cfg.DefaultCluster}
			for k, v := range s.Defaults {
				vars[k] = v
			}
			for name, p := range flagValues {
				if *p != "" {
					vars[name] = *p
				}
			}
			for name, p := range intValues {
				vars[name] = *p
			}

			sinceDur, err := time.ParseDuration(since)
			if err != nil {
				return cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --since %q: %v", since, err)
			}
			stepDur, err := time.ParseDuration(step)
			if err != nil {
				return cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --step %q: %v", step, err)
			}
			vars["since"] = since
			vars["step"] = step
			vars["since_seconds"] = strconv.Itoa(int(sinceDur.Seconds()))

			client := vmclient.New(vmclient.Options{
				BaseURL:       cfg.VMURL,
				Timeout:       cfg.Timeout,
				BasicAuthUser: cfg.BasicAuth.Username,
				BasicAuthPass: cfg.BasicAuth.Password,
			})

			env, err := Run(cmd.Context(), Inputs{
				Scenario: s,
				Vars:     vars,
				Client:   client,
				DryRun:   dryRun(),
				Since:    sinceDur,
				Step:     stepDur,
			})
			if err != nil && !isNoData(err) {
				return err
			}
			if err := output.Render(format(), env, cmd.OutOrStdout()); err != nil {
				return err
			}
			if err != nil { // NoData warning -> stderr, exit 0
				cerrors.WriteJSON(os.Stderr, err)
			}
			return nil
		},
	}

	for _, f := range s.Flags {
		switch f.Type {
		case "int":
			def := 0
			if v, ok := f.Default.(int); ok {
				def = v
			}
			p := new(int)
			*p = def
			cmd.Flags().IntVar(p, f.Name, def, fmt.Sprintf("%s (default %d)", f.Help, def))
			intValues[f.Name] = p
		default:
			def := ""
			if v, ok := f.Default.(string); ok {
				def = v
			}
			p := new(string)
			*p = def
			cmd.Flags().StringVar(p, f.Name, def, f.Help)
			flagValues[f.Name] = p
		}
	}

	if s.Range {
		cmd.Flags().StringVar(&since, "since", "5m", "Range duration (e.g. 5m, 1h)")
		cmd.Flags().StringVar(&step, "step", "30s", "Range step (e.g. 30s)")
	}
	cmd.Flags().Bool("range", s.Range, "")
	_ = cmd.Flags().MarkHidden("range")
	return cmd
}

func isNoData(err error) bool {
	var ce *cerrors.CodedError
	if !errors.As(err, &ce) {
		return false
	}
	return ce.Code == cerrors.CodeNoData
}
