package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opay-bigdata/hbase-metrics-cli/cmd/scenarios"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestRender_Goldens(t *testing.T) {
	scenarios, err := promql.LoadEmbedded()
	require.NoError(t, err)
	require.NotEmpty(t, scenarios)

	vars := promql.Vars{
		"cluster": "mrs-hbase-oline",
		"role":    "regionserver",
		"top":     5,
		"since":   "10m",
		"step":    "30s",
	}
	for _, s := range scenarios {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			rendered, err := promql.Render(s, vars)
			require.NoError(t, err)
			var b strings.Builder
			for _, r := range rendered {
				b.WriteString("# " + r.Label + "\n" + strings.TrimSpace(r.Expr) + "\n\n")
			}
			path := filepath.Join("..", "golden", s.Name+".golden.promql")
			if *update {
				require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
				return
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err, "missing golden %s — run go test -update", path)
			require.Equal(t, string(want), b.String())
		})
	}
}

func TestEnvelope_Goldens(t *testing.T) {
	all, err := promql.LoadEmbedded()
	require.NoError(t, err)
	for _, s := range all {
		s := s
		if !s.Range {
			continue
		}
		t.Run(s.Name+"_summary", func(t *testing.T) {
			env := buildFromFixture(t, s, "summary")
			compareEnvelopeGolden(t, s.Name+".summary.golden.json", env)
		})
		t.Run(s.Name+"_raw", func(t *testing.T) {
			env := buildFromFixture(t, s, "raw")
			compareEnvelopeGolden(t, s.Name+".raw.golden.json", env)
		})
	}
}

func buildFromFixture(t *testing.T, s promql.Scenario, mode string) output.Envelope {
	t.Helper()
	vars := promql.Vars{"cluster": "mrs-hbase-oline", "role": "regionserver", "top": 5, "since": "10m", "step": "30s"}
	rendered, err := promql.Render(s, vars)
	require.NoError(t, err)

	results := make([]vmclient.Result, len(rendered))
	for i, r := range rendered {
		results[i] = vmclient.Result{Result: []vmclient.Sample{
			fakeRangeSample("rs-a:19110", r.Label),
			fakeRangeSample("rs-b:19110", r.Label),
		}}
	}
	env := scenarios.BuildEnvelopeForGolden(s, rendered, results, mode)
	env.Cluster = "mrs-hbase-oline"
	return env
}

func fakeRangeSample(instance, _ string) vmclient.Sample {
	return vmclient.Sample{
		Metric: map[string]string{"instance": instance},
		Values: [][]any{
			{float64(1_700_000_000), "10"},
			{float64(1_700_000_030), "20"},
			{float64(1_700_000_060), "30"},
		},
	}
}

func compareEnvelopeGolden(t *testing.T, filename string, env output.Envelope) {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(env))
	path := filepath.Join("..", "golden", filename)
	if *update {
		require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden %s — run go test -update", path)
	require.Equal(t, string(want), buf.String())
}
