// Package promql renders scenario PromQL templates with caller-supplied vars.
package promql

import (
	"bytes"
	"text/template"
)

type Vars map[string]any

type Rendered struct {
	Label string
	Expr  string
}

func Render(s Scenario, vars Vars) ([]Rendered, error) {
	merged := map[string]any{}
	for k, v := range s.Defaults {
		merged[k] = v
	}
	for k, v := range vars {
		if v != nil && v != "" {
			merged[k] = v
		}
	}
	out := make([]Rendered, 0, len(s.Queries))
	for _, q := range s.Queries {
		t, err := template.New(q.Label).Option("missingkey=error").Parse(q.Expr)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, merged); err != nil {
			return nil, err
		}
		out = append(out, Rendered{Label: q.Label, Expr: buf.String()})
	}
	return out, nil
}
