package cmd

import (
	"runtime"
	"strings"
	"testing"
)

// TestVersionStringMatchesRootVersion guards the contract that the `version`
// subcommand and the root `--version` flag emit the same string.
func TestVersionStringMatchesRootVersion(t *testing.T) {
	got := versionString()
	if !strings.HasPrefix(got, "hbase-metrics-cli ") {
		t.Errorf("versionString() = %q, want prefix %q", got, "hbase-metrics-cli ")
	}
	if !strings.Contains(got, runtime.Version()) {
		t.Errorf("versionString() = %q, want it to contain go runtime version %q", got, runtime.Version())
	}

	root := newRootCmd()
	if root.Version != got {
		t.Errorf("root.Version = %q, want %q (must match version subcommand)", root.Version, got)
	}
}
