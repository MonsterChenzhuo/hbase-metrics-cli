//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func binaryPath(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(here), "..", "..")
	bin := filepath.Join(root, "hbase-metrics-cli")
	return bin
}

var allScenarios = []string{
	"cluster-overview", "regionserver-list", "requests-qps",
	"rpc-latency", "handler-queue", "hotspot-detect",
	"gc-pressure", "jvm-memory",
	"compaction-status", "blockcache-hitrate", "wal-stats",
	"master-status",
}

func TestDryRun_AllScenariosEmitJSONWithRenderedExpr(t *testing.T) {
	bin := binaryPath(t)
	for _, name := range allScenarios {
		name := name
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(bin, name,
				"--vm-url", "http://localhost:0",
				"--cluster", "mrs-hbase-oline",
				"--dry-run",
				"--format", "json",
			)
			var out, errb bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &errb
			require.NoError(t, cmd.Run(), "stderr=%s", errb.String())

			var env map[string]any
			require.NoError(t, json.Unmarshal(out.Bytes(), &env))
			require.Equal(t, name, env["scenario"])
			require.Equal(t, "mrs-hbase-oline", env["cluster"])
			queries := env["queries"].([]any)
			require.NotEmpty(t, queries)
			expr := queries[0].(map[string]any)["expr"].(string)
			require.Contains(t, expr, `cluster="mrs-hbase-oline"`)
		})
	}
}
