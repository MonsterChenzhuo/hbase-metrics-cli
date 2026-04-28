package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoad_FallsBackToBuiltinDefaultWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HBASE_METRICS_CLI_CONFIG_DIR", dir)

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "https://vm.example.invalid/", cfg.VMURL) // overridable default
	require.Equal(t, 10*time.Second, cfg.Timeout)
	require.Equal(t, SourceDefault, cfg.Source.VMURL)
}

func TestLoad_ReadsYAMLFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HBASE_METRICS_CLI_CONFIG_DIR", dir)

	yaml := []byte(`
vm_url: https://vm.example.com/
default_cluster: prod-1
basic_auth:
  username: alice
  password: hunter2
timeout: 20s
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), yaml, 0o600))

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "https://vm.example.com/", cfg.VMURL)
	require.Equal(t, "prod-1", cfg.DefaultCluster)
	require.Equal(t, "alice", cfg.BasicAuth.Username)
	require.Equal(t, "hunter2", cfg.BasicAuth.Password)
	require.Equal(t, 20*time.Second, cfg.Timeout)
	require.Equal(t, SourceFile, cfg.Source.VMURL)
}

func TestApplyEnv_OverridesFileValues(t *testing.T) {
	cfg := &Config{VMURL: "from-file", Source: Sources{VMURL: SourceFile}}
	t.Setenv("HBASE_VM_URL", "from-env")
	t.Setenv("HBASE_VM_USER", "u")
	t.Setenv("HBASE_VM_PASS", "p")

	ApplyEnv(cfg)
	require.Equal(t, "from-env", cfg.VMURL)
	require.Equal(t, "u", cfg.BasicAuth.Username)
	require.Equal(t, SourceEnv, cfg.Source.VMURL)
}

func TestApplyFlags_OverridesEnvAndFile(t *testing.T) {
	cfg := &Config{VMURL: "from-env", Source: Sources{VMURL: SourceEnv}}
	ApplyFlags(cfg, FlagOverrides{VMURL: "from-flag"})
	require.Equal(t, "from-flag", cfg.VMURL)
	require.Equal(t, SourceFlag, cfg.Source.VMURL)
}

func TestSave_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HBASE_METRICS_CLI_CONFIG_DIR", dir)

	in := &Config{
		VMURL:          "https://vm.example.com/",
		DefaultCluster: "c1",
		BasicAuth:      BasicAuth{Username: "u", Password: "p"},
		Timeout:        15 * time.Second,
	}
	require.NoError(t, Save(in))

	out, err := Load()
	require.NoError(t, err)
	require.Equal(t, in.VMURL, out.VMURL)
	require.Equal(t, in.DefaultCluster, out.DefaultCluster)
	require.Equal(t, in.BasicAuth, out.BasicAuth)
	require.Equal(t, in.Timeout, out.Timeout)
}

func TestValidate_RejectsEmptyVMURL(t *testing.T) {
	require.ErrorContains(t, (&Config{}).Validate(), "vm_url")
}
