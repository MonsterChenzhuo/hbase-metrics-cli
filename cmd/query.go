package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/stepauto"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

// queryHasClusterFilter is a coarse heuristic — true when the raw PromQL
// references a `cluster=` label match. Good enough to nudge users; not a
// PromQL parser. Exposed (unexported but testable in cmd package) so the
// warning logic can be unit-tested.
func queryHasClusterFilter(expr string) bool {
	return strings.Contains(expr, "cluster=") || strings.Contains(expr, "cluster!=") ||
		strings.Contains(expr, "cluster=~") || strings.Contains(expr, "cluster!~")
}

// queryColumns builds the column header for the instant query envelope: the
// fixed instance/value pair followed by every other label seen across the
// result, sorted alphabetically for deterministic table/markdown output.
// Without this the table and markdown renderers would drop labels like
// `cluster`.
func queryColumns(labelSet map[string]struct{}) []string {
	extra := make([]string, 0, len(labelSet))
	for k := range labelSet {
		extra = append(extra, k)
	}
	sort.Strings(extra)
	return append([]string{"instance", "value"}, extra...)
}

// queryInstantRows projects an instant Result into one row per series, keyed by
// instance, back-filling every label seen so table/markdown rendering keeps the
// full column schema. Returns the rows and the set of extra label keys.
func queryInstantRows(res *vmclient.Result) ([]output.Row, map[string]struct{}) {
	rows := make([]output.Row, 0, len(res.Result))
	labelSet := map[string]struct{}{}
	for _, s := range res.Result {
		row := output.Row{"instance": s.Metric["instance"]}
		if len(s.Value) >= 2 {
			row["value"] = s.Value[1]
		}
		for k, v := range s.Metric {
			if k != "instance" {
				row[k] = v
				labelSet[k] = struct{}{}
			}
		}
		rows = append(rows, row)
	}
	return rows, labelSet
}

// queryRawRows flattens a range Result into one row per (instance, timestamp),
// mirroring the raw shape scenarios emit: columns [instance, timestamp, time,
// value]. This is what makes the escape hatch usable for finding the exact
// minute of a peak — the reason instant-only query was AI-hostile.
//
// When loc is non-UTC an extra "time_local" column is emitted alongside the
// UTC "time". The UTC contract is never broken (agents can still parse "time"),
// but the operator gets the wall-clock reading in the timezone their alarm
// fired in, so no manual +08:00 arithmetic is needed to line rows up.
func queryRawRows(res *vmclient.Result, loc *time.Location) []output.Row {
	localZone := loc != nil && loc != time.UTC
	rows := []output.Row{}
	for _, s := range res.Result {
		instance := s.Metric["instance"]
		for _, v := range s.Values {
			ts, ok := querySampleTimestamp(v)
			if !ok {
				continue
			}
			row := output.Row{
				"instance":  instance,
				"timestamp": ts,
				"time":      time.Unix(ts, 0).UTC().Format(time.RFC3339),
				"value":     querySampleValue(v),
			}
			if localZone {
				row["time_local"] = time.Unix(ts, 0).In(loc).Format(time.RFC3339)
			}
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		ii, _ := rows[i]["instance"].(string)
		ij, _ := rows[j]["instance"].(string)
		if ii != ij {
			return ii < ij
		}
		ti, _ := rows[i]["timestamp"].(int64)
		tj, _ := rows[j]["timestamp"].(int64)
		return ti < tj
	})
	return rows
}

// querySampleTimestamp extracts the Unix-seconds timestamp from a VM range
// datapoint ([ <ts>, "<value>" ]). Mirrors the scenario runner's parser so
// both code paths agree on VM's numeric encodings.
func querySampleTimestamp(v []any) (int64, bool) {
	if len(v) == 0 {
		return 0, false
	}
	switch ts := v[0].(type) {
	case float64:
		return int64(ts), true
	case int64:
		return ts, true
	case json.Number:
		i, err := ts.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

// querySampleValue extracts the numeric value from a VM range datapoint,
// parsing the string form into float64 when possible.
func querySampleValue(v []any) any {
	if len(v) < 2 {
		return nil
	}
	if s, ok := v[1].(string); ok {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return v[1]
}

func newQueryCmd() *cobra.Command {
	var since string
	var step string
	var end string
	var tz string
	cmd := &cobra.Command{
		Use:   "query <promql>",
		Short: "Run a raw PromQL query (escape hatch). Instant by default; --since makes it a range query.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadEffectiveConfig()
			if err != nil {
				return err
			}
			if !queryHasClusterFilter(args[0]) && cfg.DefaultCluster != "" {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: query has no cluster filter; results may span all clusters. Add `cluster=%q` to scope it.\n",
					cfg.DefaultCluster)
			}

			// Resolve the display/parse timezone once. Empty --tz falls back
			// to UTC, so the historical behaviour is unchanged unless the
			// operator opts in.
			loc, err := parseLocation(tz)
			if err != nil {
				return cerrors.WithHint(
					cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --tz: %v", err),
					"use an IANA name like Asia/Shanghai or an offset like +08:00",
				)
			}

			hasEnd := cmd.Flags().Changed("end")

			// A range query is requested when --since is set, when --end is
			// set (pinning an absolute window), or when the global --raw flag
			// is set (raw only has meaning over a window).
			hasSince := cmd.Flags().Changed("since")
			isRange := hasSince || hasEnd || globals.Raw

			var sinceDur time.Duration
			if isRange {
				effectiveSince := since
				if effectiveSince == "" {
					effectiveSince = "5m" // --raw without --since: default window
				}
				sinceDur, err = time.ParseDuration(effectiveSince)
				if err != nil {
					return cerrors.WithHint(
						cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --since %q: %v", effectiveSince, err),
						"use a Go duration like 30m, 6h, 24h",
					)
				}
			}

			var stepDur time.Duration
			if isRange {
				if step == "" || step == "auto" {
					stepDur = stepauto.Resolve(sinceDur)
				} else {
					stepDur, err = time.ParseDuration(step)
					if err != nil {
						return cerrors.WithHint(
							cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --step %q: %v", step, err),
							"use auto or a Go duration like 30s, 5m",
						)
					}
				}
			}

			client := vmclient.New(vmclient.Options{
				BaseURL:       cfg.VMURL,
				Timeout:       cfg.Timeout,
				BasicAuthUser: cfg.BasicAuth.Username,
				BasicAuthPass: cfg.BasicAuth.Password,
			})
			ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
			defer cancel()

			env := output.Envelope{
				Scenario: "query",
				Cluster:  cfg.DefaultCluster,
				Queries:  []output.Query{{Label: "raw", Expr: args[0]}},
				Data:     []output.Row{},
			}

			if isRange {
				endTime := time.Now()
				if hasEnd {
					endTime, err = parseEndTime(end, loc)
					if err != nil {
						return cerrors.WithHint(
							cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --end: %v", err),
							"use unix seconds, RFC3339, or \"2006-01-02 15:04:05\" (interpreted in --tz)",
						)
					}
				}
				start := endTime.Add(-sinceDur)
				res, err := client.QueryRange(ctx, args[0], start, endTime, stepDur)
				if err != nil {
					return err
				}
				env.Mode = "raw"
				env.Range = &output.Range{
					Start: start.UTC().Format(time.RFC3339),
					End:   endTime.UTC().Format(time.RFC3339),
					Step:  stepDur.String(),
				}
				env.Columns = []string{"instance", "timestamp", "time", "value"}
				if loc != time.UTC {
					env.Columns = append(env.Columns, "time_local")
				}
				env.Data = queryRawRows(res, loc)
			} else {
				res, err := client.Query(ctx, args[0], time.Now())
				if err != nil {
					return err
				}
				env.Mode = "instant"
				rows, labelSet := queryInstantRows(res)
				env.Data = rows
				// Column order: instance, value, then remaining labels
				// alphabetically for deterministic output. Every row is
				// back-filled below so the header is a stable schema even
				// when a series lacks a label.
				env.Columns = queryColumns(labelSet)
			}

			if len(env.Data) == 0 {
				return cerrors.WithHint(cerrors.Errorf(cerrors.CodeNoData, "query returned no data"), "verify the PromQL expression and label values")
			}
			for _, row := range env.Data {
				for _, c := range env.Columns {
					if _, ok := row[c]; !ok {
						row[c] = nil
					}
				}
			}
			return output.Render(globals.Format, env, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "Range window (e.g. 30m, 24h) — turns the instant query into a range query")
	cmd.Flags().StringVar(&step, "step", "auto", "Range step (auto | duration like 30s, 5m); only used with --since/--raw")
	cmd.Flags().StringVar(&end, "end", "", "Window end time (unix secs, RFC3339, or \"2006-01-02 15:04:05\"); pins an absolute past window ending here. Defaults to now.")
	cmd.Flags().StringVar(&tz, "tz", "", "Timezone for --end parsing and a time_local output column (IANA name like Asia/Shanghai, or offset like +08:00). Defaults to UTC.")
	return cmd
}
