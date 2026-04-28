package golden

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
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
