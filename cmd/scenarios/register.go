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
	"github.com/opay-bigdata/hbase-metrics-cli/internal/stepauto"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

// LoadConfigFn is wired by the cmd package to break an import cycle.
type LoadConfigFn func() (*config.Config, error)

// FormatFn returns the global --format value.
type FormatFn func() string

// DryRunFn returns the global --dry-run flag.
type DryRunFn func() bool

// RawFn returns the global --raw flag.
type RawFn func() bool

func Register(root *cobra.Command, loadCfg LoadConfigFn, format FormatFn, dryRun DryRunFn, raw RawFn) error {
	scenarios, err := promql.LoadEmbedded()
	if err != nil {
		return err
	}
	for _, s := range scenarios {
		root.AddCommand(buildCmd(s, loadCfg, format, dryRun, raw))
	}
	return nil
}

func buildCmd(s promql.Scenario, loadCfg LoadConfigFn, format FormatFn, dryRun DryRunFn, raw RawFn) *cobra.Command {
	flagValues := map[string]*string{}
	intValues := map[string]*int{}
	since := ""
	step := ""
	rangeCapable := s.Range || s.InstantSummary

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

			hasSince := cmd.Flags().Changed("since")
			if hasSince && !rangeCapable {
				return cerrors.WithHint(
					cerrors.Errorf(cerrors.CodeFlagInvalid, "scenario %q is instant; --since does not apply", s.Name),
					"omit --since for instant scenarios; or use a time-range scenario like rpc-latency / gc-pressure",
				)
			}

			effectiveSince := since
			if effectiveSince == "" && s.Range {
				effectiveSince = "5m"
			}
			var sinceDur time.Duration
			if effectiveSince != "" {
				sinceDur, err = time.ParseDuration(effectiveSince)
				if err != nil {
					return cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --since %q: %v", effectiveSince, err)
				}
			}

			var stepDur time.Duration
			if step == "" || step == "auto" {
				stepDur = stepauto.Resolve(sinceDur)
			} else {
				stepDur, err = time.ParseDuration(step)
				if err != nil {
					return cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --step %q: %v", step, err)
				}
			}

			vars["since"] = effectiveSince
			vars["step"] = stepDur.String()
			if sinceDur > 0 {
				vars["since_seconds"] = strconv.Itoa(int(sinceDur.Seconds()))
			}

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
				Raw:      raw(),
				HasSince: hasSince,
				Since:    sinceDur,
				Step:     stepDur,
			})
			if err != nil && !isNoData(err) {
				return err
			}
			if err := output.Render(format(), env, cmd.OutOrStdout()); err != nil {
				return err
			}
			if err != nil {
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

	cmd.Flags().StringVar(&since, "since", "", sinceFlagHelp(rangeCapable))
	if rangeCapable {
		cmd.Flags().StringVar(&step, "step", "auto", "Range step (auto | duration like 30s, 5m)")
	}
	cmd.Flags().Bool("range", s.Range, "")
	_ = cmd.Flags().MarkHidden("range")
	return cmd
}

func sinceFlagHelp(rangeCapable bool) string {
	if rangeCapable {
		return "Range duration (e.g. 30m, 24h)"
	}
	return "Not applicable for instant scenarios — omit"
}

func isNoData(err error) bool {
	var ce *cerrors.CodedError
	if !errors.As(err, &ce) {
		return false
	}
	return ce.Code == cerrors.CodeNoData
}
