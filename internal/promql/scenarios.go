package promql

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	scenariosdata "github.com/opay-bigdata/hbase-metrics-cli/scenarios"
)

type Flag struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Default any      `yaml:"default"`
	Enum    []string `yaml:"enum"`
	Help    string   `yaml:"help"`
}

type Query struct {
	Label string `yaml:"label"`
	Expr  string `yaml:"expr"`
}

type SummarySpec struct {
	Aggs []string `yaml:"aggs"`
}

type Scenario struct {
	Name           string                 `yaml:"name"`
	Description    string                 `yaml:"description"`
	Range          bool                   `yaml:"range"`
	InstantSummary bool                   `yaml:"instant_summary"`
	Defaults       map[string]any         `yaml:"defaults"`
	Flags          []Flag                 `yaml:"flags"`
	Queries        []Query                `yaml:"queries"`
	Columns        []string               `yaml:"columns"`
	SummaryColumns []string               `yaml:"summary_columns"`
	Summary        map[string]SummarySpec `yaml:"summary"`
}

var validAggs = map[string]struct{}{
	"count": {}, "min": {}, "max": {}, "avg": {},
	"p50": {}, "p95": {}, "p99": {}, "last": {},
}

// LoadEmbedded loads every scenario YAML compiled into the binary,
// skipping files whose name starts with `_` (placeholders / disabled).
func LoadEmbedded() ([]Scenario, error) {
	return loadFS(scenariosdata.FS)
}

func loadFS(efs fs.FS) ([]Scenario, error) {
	entries, err := fs.ReadDir(efs, ".")
	if err != nil {
		return nil, fmt.Errorf("read scenarios FS: %w", err)
	}
	var out []Scenario
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		b, err := fs.ReadFile(efs, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		s, err := ParseScenario(b)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func ParseScenario(b []byte) (Scenario, error) {
	var s Scenario
	if err := yaml.Unmarshal(b, &s); err != nil {
		return Scenario{}, err
	}
	if s.Name == "" {
		return Scenario{}, fmt.Errorf("scenario missing name")
	}
	if len(s.Queries) == 0 {
		return Scenario{}, fmt.Errorf("scenario %s has no queries", s.Name)
	}
	for label, spec := range s.Summary {
		for _, agg := range spec.Aggs {
			if _, ok := validAggs[agg]; !ok {
				return Scenario{}, fmt.Errorf("scenario %s query %q: unknown agg %q (valid: count,min,max,avg,p50,p95,p99,last)", s.Name, label, agg)
			}
		}
	}
	return s, nil
}
