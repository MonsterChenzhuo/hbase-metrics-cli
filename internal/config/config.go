// Package config loads and saves the hbase-metrics-cli configuration with
// layered overrides: flag > env > YAML file > compile-time default.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
)

const (
	defaultVMURL    = "https://vm.example.invalid/"
	defaultCluster  = ""
	defaultTimeout  = 10 * time.Second
	configFileName  = "config.yaml"
)

type Source string

const (
	SourceDefault Source = "default"
	SourceFile    Source = "file"
	SourceEnv     Source = "env"
	SourceFlag    Source = "flag"
)

type BasicAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Sources struct {
	VMURL          Source `yaml:"-"`
	DefaultCluster Source `yaml:"-"`
	BasicAuth      Source `yaml:"-"`
	Timeout        Source `yaml:"-"`
}

type Config struct {
	VMURL          string        `yaml:"vm_url"`
	DefaultCluster string        `yaml:"default_cluster"`
	BasicAuth      BasicAuth     `yaml:"basic_auth"`
	Timeout        time.Duration `yaml:"timeout"`

	Source Sources `yaml:"-"`
}

type FlagOverrides struct {
	VMURL          string
	DefaultCluster string
	BasicAuthUser  string
	BasicAuthPass  string
	Timeout        time.Duration
}

func ConfigDir() (string, error) {
	if v := os.Getenv("HBASE_METRICS_CLI_CONFIG_DIR"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "hbase-metrics-cli"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

func defaults() *Config {
	return &Config{
		VMURL:          defaultVMURL,
		DefaultCluster: defaultCluster,
		Timeout:        defaultTimeout,
		Source: Sources{
			VMURL:          SourceDefault,
			DefaultCluster: SourceDefault,
			BasicAuth:      SourceDefault,
			Timeout:        SourceDefault,
		},
	}
}

func Load() (*Config, error) {
	cfg := defaults()
	path, err := ConfigPath()
	if err != nil {
		return nil, cerrors.Errorf(cerrors.CodeConfigInvalid, "resolve config dir: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, cerrors.Errorf(cerrors.CodeConfigInvalid, "read %s: %v", path, err)
	}
	var fileCfg Config
	if err := yaml.Unmarshal(b, &fileCfg); err != nil {
		return nil, cerrors.Errorf(cerrors.CodeConfigInvalid, "parse %s: %v", path, err)
	}
	if fileCfg.VMURL != "" {
		cfg.VMURL = fileCfg.VMURL
		cfg.Source.VMURL = SourceFile
	}
	if fileCfg.DefaultCluster != "" {
		cfg.DefaultCluster = fileCfg.DefaultCluster
		cfg.Source.DefaultCluster = SourceFile
	}
	if fileCfg.BasicAuth.Username != "" || fileCfg.BasicAuth.Password != "" {
		cfg.BasicAuth = fileCfg.BasicAuth
		cfg.Source.BasicAuth = SourceFile
	}
	if fileCfg.Timeout > 0 {
		cfg.Timeout = fileCfg.Timeout
		cfg.Source.Timeout = SourceFile
	}
	return cfg, nil
}

func ApplyEnv(cfg *Config) {
	if v := os.Getenv("HBASE_VM_URL"); v != "" {
		cfg.VMURL = v
		cfg.Source.VMURL = SourceEnv
	}
	if v := os.Getenv("HBASE_CLUSTER"); v != "" {
		cfg.DefaultCluster = v
		cfg.Source.DefaultCluster = SourceEnv
	}
	if v := os.Getenv("HBASE_VM_USER"); v != "" {
		cfg.BasicAuth.Username = v
		cfg.Source.BasicAuth = SourceEnv
	}
	if v := os.Getenv("HBASE_VM_PASS"); v != "" {
		cfg.BasicAuth.Password = v
		cfg.Source.BasicAuth = SourceEnv
	}
	if v := os.Getenv("HBASE_VM_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Timeout = d
			cfg.Source.Timeout = SourceEnv
		}
	}
}

func ApplyFlags(cfg *Config, f FlagOverrides) {
	if f.VMURL != "" {
		cfg.VMURL = f.VMURL
		cfg.Source.VMURL = SourceFlag
	}
	if f.DefaultCluster != "" {
		cfg.DefaultCluster = f.DefaultCluster
		cfg.Source.DefaultCluster = SourceFlag
	}
	if f.BasicAuthUser != "" {
		cfg.BasicAuth.Username = f.BasicAuthUser
		cfg.Source.BasicAuth = SourceFlag
	}
	if f.BasicAuthPass != "" {
		cfg.BasicAuth.Password = f.BasicAuthPass
		cfg.Source.BasicAuth = SourceFlag
	}
	if f.Timeout > 0 {
		cfg.Timeout = f.Timeout
		cfg.Source.Timeout = SourceFlag
	}
}

func (c *Config) Validate() error {
	if c.VMURL == "" {
		return cerrors.Errorf(cerrors.CodeConfigInvalid, "vm_url is required")
	}
	if _, err := url.Parse(c.VMURL); err != nil {
		return cerrors.Errorf(cerrors.CodeConfigInvalid, "vm_url is not a valid URL: %v", err)
	}
	if c.Timeout <= 0 {
		return cerrors.Errorf(cerrors.CodeConfigInvalid, "timeout must be positive")
	}
	return nil
}

func Save(cfg *Config) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, configFileName)
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, b, 0o600)
}
