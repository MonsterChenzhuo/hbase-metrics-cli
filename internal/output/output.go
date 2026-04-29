// Package output renders Envelopes to json, table, or markdown.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
)

type Range struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Step  string `json:"step"`
}

type Query struct {
	Label string `json:"label"`
	Expr  string `json:"expr"`
}

type Row map[string]any

type Envelope struct {
	Scenario string   `json:"scenario"`
	Cluster  string   `json:"cluster"`
	Mode     string   `json:"mode"`
	Range    *Range   `json:"range,omitempty"`
	Queries  []Query  `json:"queries"`
	Columns  []string `json:"columns,omitempty"`
	Data     []Row    `json:"data"`
}

func Render(format string, env Envelope, w io.Writer) error {
	switch format {
	case "json", "":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(env)
	case "table":
		return renderTable(env, w)
	case "markdown":
		return renderMarkdown(env, w)
	default:
		return cerrors.Errorf(cerrors.CodeFlagInvalid, "unknown format %q (allowed: json, table, markdown)", format)
	}
}

func columns(env Envelope) []string {
	if len(env.Columns) > 0 {
		return env.Columns
	}
	if len(env.Data) == 0 {
		return nil
	}
	cols := make([]string, 0, len(env.Data[0]))
	for k := range env.Data[0] {
		cols = append(cols, k)
	}
	return cols
}

func renderTable(env Envelope, w io.Writer) error {
	cols := columns(env)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = strings.ToUpper(c)
	}
	if _, err := fmt.Fprintln(tw, strings.Join(headers, "\t")); err != nil {
		return err
	}
	for _, row := range env.Data {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = fmt.Sprintf("%v", row[c])
		}
		if _, err := fmt.Fprintln(tw, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func renderMarkdown(env Envelope, w io.Writer) error {
	cols := columns(env)
	if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(cols, " | ")); err != nil {
		return err
	}
	sep := strings.Repeat("--- | ", len(cols))
	if _, err := fmt.Fprintf(w, "| %s\n", strings.TrimRight(sep, " ")); err != nil {
		return err
	}
	for _, row := range env.Data {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = fmt.Sprintf("%v", row[c])
		}
		if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(cells, " | ")); err != nil {
			return err
		}
	}
	return nil
}
