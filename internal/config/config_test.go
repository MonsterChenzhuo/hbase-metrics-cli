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

func TestLoad_ReadsEnvsMap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HBASE_METRICS_CLI_CONFIG_DIR", dir)

	yaml := []byte(`
active_env: nigeria
envs:
  nigeria:
    vm_url: http://ng.example.com/
    default_cluster: mrs-hbase-oline-ng
  indonesia:
    vm_url: https://id.example.com/
    default_cluster: mrs-hbase-oline
vm_url: http://fallback.example.com/
default_cluster: fallback-cluster
timeout: 8s
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), yaml, 0o600))

	cfg, err := Load()
	require.NoError(t, err)

	// Load itself does NOT auto-overlay the active profile — that's the
	// caller's job (root.go decides between flag/env var/active_env).
	require.Equal(t, "http://fallback.example.com/", cfg.VMURL)
	require.Equal(t, "fallback-cluster", cfg.DefaultCluster)
	require.Equal(t, SourceFile, cfg.Source.VMURL)
	require.Empty(t, cfg.SelectedEnv)

	require.Equal(t, "nigeria", cfg.ActiveEnv)
	require.Len(t, cfg.Envs, 2)
	require.Equal(t, "http://ng.example.com/", cfg.Envs["nigeria"].VMURL)
	require.Equal(t, "mrs-hbase-oline-ng", cfg.Envs["nigeria"].DefaultCluster)
	require.Equal(t, "https://id.example.com/", cfg.Envs["indonesia"].VMURL)
	require.Equal(t, "mrs-hbase-oline", cfg.Envs["indonesia"].DefaultCluster)
}

func TestApplyEnvProfile_OverridesTopLevel(t *testing.T) {
	cfg := &Config{
		VMURL:          "fallback",
		DefaultCluster: "fallback-cluster",
		Source: Sources{
			VMURL:          SourceFile,
			DefaultCluster: SourceFile,
		},
		Envs: map[string]EnvConfig{
			"indonesia": {VMURL: "https://id.example.com/", DefaultCluster: "mrs-hbase-oline"},
		},
	}

	require.NoError(t, ApplyEnvProfile(cfg, "indonesia"))
	require.Equal(t, "https://id.example.com/", cfg.VMURL)
	require.Equal(t, "mrs-hbase-oline", cfg.DefaultCluster)
	require.Equal(t, SourceEnvProfile, cfg.Source.VMURL)
	require.Equal(t, SourceEnvProfile, cfg.Source.DefaultCluster)
	require.Equal(t, "indonesia", cfg.SelectedEnv)
}

func TestApplyEnvProfile_UnknownNameErrors(t *testing.T) {
	cfg := &Config{Envs: map[string]EnvConfig{"nigeria": {VMURL: "u"}}}
	err := ApplyEnvProfile(cfg, "atlantis")
	require.Error(t, err)
	require.Contains(t, err.Error(), "atlantis")
	require.Empty(t, cfg.SelectedEnv)
}

func TestApplyEnvProfile_PreservesUnsetFields(t *testing.T) {
	cfg := &Config{
		VMURL:          "fallback",
		DefaultCluster: "fallback-cluster",
		BasicAuth:      BasicAuth{Username: "preserved", Password: "preserved"},
		Timeout:        7 * time.Second,
		Source: Sources{
			VMURL:          SourceFile,
			DefaultCluster: SourceFile,
			BasicAuth:      SourceFile,
			Timeout:        SourceFile,
		},
		Envs: map[string]EnvConfig{
			// only VMURL is set in the profile; the rest must survive
			"sparse": {VMURL: "https://sparse.example.com/"},
		},
	}
	require.NoError(t, ApplyEnvProfile(cfg, "sparse"))
	require.Equal(t, "https://sparse.example.com/", cfg.VMURL)
	require.Equal(t, "fallback-cluster", cfg.DefaultCluster)
	require.Equal(t, "preserved", cfg.BasicAuth.Username)
	require.Equal(t, 7*time.Second, cfg.Timeout)
	require.Equal(t, SourceFile, cfg.Source.BasicAuth)
	require.Equal(t, SourceFile, cfg.Source.Timeout)
	require.Equal(t, SourceFile, cfg.Source.DefaultCluster)
}

func TestApplyEnvProfile_EmptyNameNoOp(t *testing.T) {
	cfg := &Config{VMURL: "fallback", Source: Sources{VMURL: SourceFile}}
	require.NoError(t, ApplyEnvProfile(cfg, ""))
	require.Equal(t, "fallback", cfg.VMURL)
	require.Equal(t, SourceFile, cfg.Source.VMURL)
	require.Empty(t, cfg.SelectedEnv)
}

func TestEnvProfile_FlagOverridesProfileValue(t *testing.T) {
	cfg := &Config{
		VMURL:  "fallback",
		Source: Sources{VMURL: SourceFile},
		Envs:   map[string]EnvConfig{"id": {VMURL: "https://id.example.com/"}},
	}
	require.NoError(t, ApplyEnvProfile(cfg, "id"))
	require.Equal(t, SourceEnvProfile, cfg.Source.VMURL)

	ApplyFlags(cfg, FlagOverrides{VMURL: "https://flag.example.com/"})
	require.Equal(t, "https://flag.example.com/", cfg.VMURL)
	require.Equal(t, SourceFlag, cfg.Source.VMURL)
}

func TestEnvNames_SortedAndStable(t *testing.T) {
	cfg := &Config{Envs: map[string]EnvConfig{
		"nigeria":   {},
		"indonesia": {},
		"colombia":  {},
	}}
	require.Equal(t, []string{"colombia", "indonesia", "nigeria"}, EnvNames(cfg))
}

func TestEnvNames_EmptyReturnsNil(t *testing.T) {
	require.Nil(t, EnvNames(&Config{}))
}
