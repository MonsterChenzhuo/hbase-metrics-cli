# hbase-metrics-cli Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI that lets Claude Code diagnose HBase clusters by running 12 named scenario commands against VictoriaMetrics, returning structured JSON / table / markdown output.

**Architecture:** Cobra CLI; embedded YAML scenario templates rendered via `text/template`; thin internal packages (`config`, `vmclient`, `promql`, `output`, `errors`); auto-registered scenario commands walk an `embed.FS`; structured stderr errors with actionable hints.

**Tech Stack:** Go 1.23+, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`, `golang.org/x/sync/errgroup`, `github.com/stretchr/testify`, GoReleaser, golangci-lint.

**Reference spec:** `docs/superpowers/specs/2026-04-28-hbase-metrics-cli-design.md`

**Working directory for all tasks:** `/Users/opay-20240095/IdeaProjects/createcli/hbase-metrics-cli`

**Module path used in this plan:** `github.com/opay-bigdata/hbase-metrics-cli` — replace at Task 1 if a different owner is preferred (then `find . -name '*.go' | xargs sed -i '' 's|github.com/opay-bigdata/hbase-metrics-cli|<new>|g'`).

---

## Task 1: Project skeleton

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `Makefile`
- Create: `.gitignore`
- Create: `.golangci.yml`

- [ ] **Step 1: Initialize Go module**

Run from `hbase-metrics-cli/`:

```bash
go mod init github.com/opay-bigdata/hbase-metrics-cli
go get github.com/spf13/cobra@v1.10.2
go get github.com/stretchr/testify@v1.11.1
go get gopkg.in/yaml.v3@v3.0.1
go get golang.org/x/sync@v0.15.0
```

Expected: `go.mod` and `go.sum` created.

- [ ] **Step 2: Write `main.go`**

```go
// Copyright (c) 2026 OPay Bigdata.
// SPDX-License-Identifier: MIT
package main

import (
	"os"

	"github.com/opay-bigdata/hbase-metrics-cli/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
```

- [ ] **Step 3: Write `.gitignore`**

```gitignore
# Binaries
/hbase-metrics-cli
/dist/

# Test artifacts
/coverage.out
/*.test

# Editor
.idea/
.vscode/
*.swp
.DS_Store

# Local config
config.local.yaml
```

- [ ] **Step 4: Write `Makefile`**

```makefile
GO ?= go
BINARY := hbase-metrics-cli
PKG := github.com/opay-bigdata/hbase-metrics-cli
LDFLAGS := -s -w -X $(PKG)/cmd.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build install unit-test e2e-dry test lint fmt tidy clean release-snapshot

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install:
	$(GO) install -ldflags "$(LDFLAGS)" .

unit-test:
	$(GO) test -race -count=1 ./...

e2e-dry: build
	$(GO) test -race -count=1 -tags=e2e ./tests/e2e/...

test: unit-test e2e-dry

lint:
	$(GO) vet ./...
	@gofmt -l . | tee /dev/stderr | (! read)
	@$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6 run

fmt:
	gofmt -w .

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BINARY)
	rm -rf dist/

release-snapshot:
	$(GO) run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
```

- [ ] **Step 5: Write `.golangci.yml`**

```yaml
version: "2"
run:
  timeout: 5m
linters:
  enable:
    - errcheck
    - govet
    - ineffassign
    - staticcheck
    - unused
    - gosimple
    - misspell
    - gofmt
    - goimports
    - revive
  settings:
    revive:
      rules:
        - name: exported
          arguments: ["disableStutteringCheck"]
issues:
  exclude-dirs:
    - dist
```

- [ ] **Step 6: Create stub `cmd/root.go` so `go build` succeeds**

```go
package cmd

import "fmt"

var version = "dev"

// Execute is the package entry point invoked by main.go.
func Execute() int {
	fmt.Println("hbase-metrics-cli", version)
	return 0
}
```

- [ ] **Step 7: Verify build and commit**

```bash
go build ./...
go vet ./...
git init && git add . && git commit -m "chore: project skeleton"
```

Expected: build succeeds, vet clean.

---

## Task 2: `internal/errors` — structured stderr errors

**Files:**
- Create: `internal/errors/errors.go`
- Create: `internal/errors/errors_test.go`

- [ ] **Step 1: Write the failing test**

`internal/errors/errors_test.go`:

```go
package errors

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrorf_FormatsWithCode(t *testing.T) {
	err := Errorf(CodeFlagInvalid, "bad role %q", "foo")
	require.Equal(t, "bad role \"foo\"", err.Error())

	var ce *CodedError
	require.True(t, errors.As(err, &ce))
	require.Equal(t, CodeFlagInvalid, ce.Code)
	require.Equal(t, ExitUserError, ce.ExitCode())
}

func TestWithHint_AttachesHintAndPreservesCode(t *testing.T) {
	err := WithHint(Errorf(CodeVMHTTP4XX, "401"), "set HBASE_VM_USER")
	var ce *CodedError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, "set HBASE_VM_USER", ce.Hint)
	require.Equal(t, CodeVMHTTP4XX, ce.Code)
}

func TestWriteJSON_EmitsEnvelope(t *testing.T) {
	err := WithHint(Errorf(CodeVMHTTP5XX, "VictoriaMetrics returned 502"), "retry")
	var buf bytes.Buffer
	WriteJSON(&buf, err)

	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, "VM_HTTP_5XX", got["error"]["code"])
	require.Equal(t, "VictoriaMetrics returned 502", got["error"]["message"])
	require.Equal(t, "retry", got["error"]["hint"])
}

func TestExitCode_DefaultsToOneForUnknownErrors(t *testing.T) {
	require.Equal(t, ExitInternal, ExitCode(errors.New("plain")))
}

func TestExitCode_ZeroForNoData(t *testing.T) {
	err := Errorf(CodeNoData, "empty result")
	require.Equal(t, 0, ExitCode(err))
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./internal/errors/...
```

Expected: compile errors (package not yet implemented).

- [ ] **Step 3: Implement `internal/errors/errors.go`**

```go
// Package errors provides structured CLI errors with stable codes,
// actionable hints, and JSON serialization for stderr.
package errors

import (
	"encoding/json"
	"fmt"
	"io"
)

type Code string

const (
	CodeConfigMissing Code = "CONFIG_MISSING"
	CodeConfigInvalid Code = "CONFIG_INVALID"
	CodeFlagInvalid   Code = "FLAG_INVALID"
	CodeVMUnreachable Code = "VM_UNREACHABLE"
	CodeVMHTTP4XX     Code = "VM_HTTP_4XX"
	CodeVMHTTP5XX     Code = "VM_HTTP_5XX"
	CodeNoData        Code = "NO_DATA"
	CodeInternal      Code = "INTERNAL"
)

const (
	ExitOK         = 0
	ExitInternal   = 1
	ExitUserError  = 2
	ExitVMFailure  = 3
)

type CodedError struct {
	Code    Code
	Message string
	Hint    string
}

func (e *CodedError) Error() string { return e.Message }

func (e *CodedError) ExitCode() int {
	switch e.Code {
	case CodeConfigMissing, CodeConfigInvalid, CodeFlagInvalid:
		return ExitUserError
	case CodeVMUnreachable, CodeVMHTTP4XX, CodeVMHTTP5XX:
		return ExitVMFailure
	case CodeNoData:
		return ExitOK
	case CodeInternal:
		return ExitInternal
	default:
		return ExitInternal
	}
}

func Errorf(code Code, format string, args ...any) error {
	return &CodedError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func WithHint(err error, hint string) error {
	if ce, ok := err.(*CodedError); ok {
		ce.Hint = hint
		return ce
	}
	return &CodedError{Code: CodeInternal, Message: err.Error(), Hint: hint}
}

type envelope struct {
	Error payload `json:"error"`
}

type payload struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func WriteJSON(w io.Writer, err error) {
	ce, ok := err.(*CodedError)
	if !ok {
		ce = &CodedError{Code: CodeInternal, Message: err.Error()}
	}
	_ = json.NewEncoder(w).Encode(envelope{Error: payload{
		Code: ce.Code, Message: ce.Message, Hint: ce.Hint,
	}})
}

func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	if ce, ok := err.(*CodedError); ok {
		return ce.ExitCode()
	}
	return ExitInternal
}
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/errors/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/errors
git commit -m "feat(errors): add structured CLI errors with codes, hints, JSON envelope"
```

---

## Task 3: `internal/config` — layered config (flag > env > file > default)

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

`internal/config/config_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./internal/config/...
```

Expected: compile errors.

- [ ] **Step 3: Implement `internal/config/config.go`**

```go
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
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/config/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat(config): add layered config (flag > env > file > default)"
```

---

## Task 4: `internal/output` — JSON / table / markdown rendering

**Files:**
- Create: `internal/output/output.go`
- Create: `internal/output/output_test.go`

- [ ] **Step 1: Write the failing test**

`internal/output/output_test.go`:

```go
package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sampleEnvelope() Envelope {
	return Envelope{
		Scenario: "rpc-latency",
		Cluster:  "mrs-hbase-oline",
		Range:    &Range{Start: "t0", End: "t1", Step: "30s"},
		Queries: []Query{
			{Label: "p99", Expr: "topk(5, hadoop_hbase_p99{...})"},
		},
		Columns: []string{"instance", "p99"},
		Data: []Row{
			{"instance": "10.0.0.1:19110", "p99": 12.3},
			{"instance": "10.0.0.2:19110", "p99": 9.8},
		},
	}
}

func TestRender_JSON(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Render("json", sampleEnvelope(), &buf))

	var got Envelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, "rpc-latency", got.Scenario)
	require.Len(t, got.Data, 2)
}

func TestRender_Table_HasHeaderAndRows(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Render("table", sampleEnvelope(), &buf))
	out := buf.String()
	require.Contains(t, out, "INSTANCE")
	require.Contains(t, out, "P99")
	require.Contains(t, out, "10.0.0.1:19110")
}

func TestRender_Markdown_HasPipeTable(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Render("markdown", sampleEnvelope(), &buf))
	out := buf.String()
	require.True(t, strings.Contains(out, "| instance | p99 |"))
	require.Contains(t, out, "| --- | --- |")
}

func TestRender_UnknownFormat(t *testing.T) {
	require.Error(t, Render("xml", sampleEnvelope(), &bytes.Buffer{}))
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./internal/output/...
```

Expected: compile errors.

- [ ] **Step 3: Implement `internal/output/output.go`**

```go
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
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/output/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/output
git commit -m "feat(output): render envelopes as json/table/markdown"
```

---

## Task 5: `internal/vmclient` — VictoriaMetrics HTTP client

**Files:**
- Create: `internal/vmclient/vmclient.go`
- Create: `internal/vmclient/vmclient_test.go`

- [ ] **Step 1: Write the failing test**

`internal/vmclient/vmclient_test.go`:

```go
package vmclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuery_BuildsCorrectURLAndParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/query", r.URL.Path)
		require.Equal(t, `up{cluster="c1"}`, r.URL.Query().Get("query"))
		require.Equal(t, "1714291200", r.URL.Query().Get("time"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"a"},"value":[1714291200,"1"]}]}}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	ts := time.Unix(1714291200, 0)
	res, err := c.Query(context.Background(), `up{cluster="c1"}`, ts)
	require.NoError(t, err)
	require.Equal(t, "vector", res.ResultType)
	require.Len(t, res.Result, 1)
	require.Equal(t, "a", res.Result[0].Metric["instance"])
}

func TestQueryRange_BuildsCorrectURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/query_range", r.URL.Path)
		require.NotEmpty(t, r.URL.Query().Get("start"))
		require.NotEmpty(t, r.URL.Query().Get("end"))
		require.Equal(t, "30", r.URL.Query().Get("step"))
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	now := time.Unix(1714291200, 0)
	_, err := c.QueryRange(context.Background(), "up", now.Add(-5*time.Minute), now, 30*time.Second)
	require.NoError(t, err)
}

func TestQuery_Sends401AsVMHTTP4XX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := c.Query(context.Background(), "up", time.Now())
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
}

func TestQuery_AppliesBasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "alice", u)
		require.Equal(t, "hunter2", p)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second, BasicAuthUser: "alice", BasicAuthPass: "hunter2"})
	_, err := c.Query(context.Background(), "up", time.Now())
	require.NoError(t, err)
}

func TestStatusFailedReturns4XX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error"}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := c.Query(context.Background(), "bad{", time.Now())
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse error")
}

// guard for unused import on Go versions
var _ = json.RawMessage{}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./internal/vmclient/...
```

Expected: compile errors.

- [ ] **Step 3: Implement `internal/vmclient/vmclient.go`**

```go
// Package vmclient is a small HTTP client for VictoriaMetrics' Prometheus API.
package vmclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
)

type Options struct {
	BaseURL       string
	Timeout       time.Duration
	BasicAuthUser string
	BasicAuthPass string
	UserAgent     string
}

type Client struct {
	opts Options
	hc   *http.Client
}

func New(opts Options) *Client {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "hbase-metrics-cli"
	}
	return &Client{opts: opts, hc: &http.Client{Timeout: opts.Timeout}}
}

type Sample struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value,omitempty"`  // instant
	Values [][]any           `json:"values,omitempty"` // range
}

type Result struct {
	ResultType string   `json:"resultType"`
	Result     []Sample `json:"result"`
}

type apiResponse struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
}

func (c *Client) Query(ctx context.Context, expr string, ts time.Time) (*Result, error) {
	q := url.Values{}
	q.Set("query", expr)
	q.Set("time", strconv.FormatInt(ts.Unix(), 10))
	return c.do(ctx, "/api/v1/query", q)
}

func (c *Client) QueryRange(ctx context.Context, expr string, start, end time.Time, step time.Duration) (*Result, error) {
	q := url.Values{}
	q.Set("query", expr)
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	q.Set("step", strconv.FormatFloat(step.Seconds(), 'f', -1, 64))
	return c.do(ctx, "/api/v1/query_range", q)
}

func (c *Client) do(ctx context.Context, path string, q url.Values) (*Result, error) {
	base := strings.TrimRight(c.opts.BaseURL, "/")
	full := base + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, cerrors.Errorf(cerrors.CodeInternal, "build request: %v", err)
	}
	req.Header.Set("User-Agent", c.opts.UserAgent)
	if c.opts.BasicAuthUser != "" || c.opts.BasicAuthPass != "" {
		req.SetBasicAuth(c.opts.BasicAuthUser, c.opts.BasicAuthPass)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, cerrors.Errorf(cerrors.CodeVMUnreachable, "%s: %v", base, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		return nil, cerrors.Errorf(cerrors.CodeVMHTTP5XX, "%s returned %d: %s", base, resp.StatusCode, truncate(body, 200))
	}
	if resp.StatusCode >= 400 {
		return nil, cerrors.Errorf(cerrors.CodeVMHTTP4XX, "%s returned %d: %s", base, resp.StatusCode, truncate(body, 200))
	}

	var ar apiResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, cerrors.Errorf(cerrors.CodeVMHTTP5XX, "decode response: %v", err)
	}
	if ar.Status != "success" {
		return nil, cerrors.Errorf(cerrors.CodeVMHTTP4XX, "%s: %s", ar.ErrorType, ar.Error)
	}
	var res Result
	if err := json.Unmarshal(ar.Data, &res); err != nil {
		return nil, cerrors.Errorf(cerrors.CodeVMHTTP5XX, "decode data: %v", err)
	}
	return &res, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return fmt.Sprintf("%s...(truncated)", b[:n])
}
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/vmclient/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vmclient
git commit -m "feat(vmclient): add VictoriaMetrics query/query_range client"
```

---

## Task 6: `internal/promql` — load embedded scenarios + render templates

**Files:**
- Create: `scenarios/_meta.yaml` (placeholder so embed.FS isn't empty before Task 10)
- Create: `internal/promql/scenarios.go`
- Create: `internal/promql/promql.go`
- Create: `internal/promql/promql_test.go`

- [ ] **Step 1: Add a placeholder embed source so `internal/promql` compiles**

`scenarios/_meta.yaml`:

```yaml
# Placeholder — real scenarios live in cluster-overview.yaml, rpc-latency.yaml, ...
# This file is ignored by the loader (filename starts with `_`).
```

- [ ] **Step 2: Write the failing test**

`internal/promql/promql_test.go`:

```go
package promql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_FiltersUnderscoreFiles(t *testing.T) {
	scenarios, err := LoadEmbedded()
	require.NoError(t, err)
	for _, s := range scenarios {
		require.NotEqual(t, '_', s.Name[0])
	}
}

func TestRender_SubstitutesClusterAndRole(t *testing.T) {
	s := Scenario{
		Name: "demo",
		Queries: []Query{
			{Label: "p99", Expr: `topk({{.top}}, my_metric{cluster="{{.cluster}}", role="{{.role}}"})`},
		},
		Defaults: map[string]any{"top": 10},
	}
	rendered, err := Render(s, Vars{"cluster": "c1", "role": "regionserver"})
	require.NoError(t, err)
	require.Len(t, rendered, 1)
	require.Equal(t, "p99", rendered[0].Label)
	require.Contains(t, rendered[0].Expr, `cluster="c1"`)
	require.Contains(t, rendered[0].Expr, `role="regionserver"`)
	require.Contains(t, rendered[0].Expr, "topk(10,")
}

func TestRender_UnknownTemplateVarFails(t *testing.T) {
	s := Scenario{
		Name:    "demo",
		Queries: []Query{{Label: "x", Expr: `{{.missing}}`}},
	}
	_, err := Render(s, Vars{})
	require.Error(t, err)
}

func TestParseScenario_ValidatesRequiredFields(t *testing.T) {
	_, err := ParseScenario([]byte(`name: ""`))
	require.Error(t, err)
}
```

- [ ] **Step 3: Run the test to confirm it fails**

```bash
go test ./internal/promql/...
```

Expected: compile errors.

- [ ] **Step 4: Implement `internal/promql/scenarios.go`**

```go
package promql

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed all:../../scenarios
var scenariosFS embed.FS

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

type Scenario struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Range       bool           `yaml:"range"`
	Defaults    map[string]any `yaml:"defaults"`
	Flags       []Flag         `yaml:"flags"`
	Queries     []Query        `yaml:"queries"`
	Columns     []string       `yaml:"columns"`
}

func LoadEmbedded() ([]Scenario, error) {
	entries, err := fs.ReadDir(scenariosFS, "../../scenarios")
	if err != nil {
		return nil, fmt.Errorf("read embedded scenarios: %w", err)
	}
	var out []Scenario
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		b, err := fs.ReadFile(scenariosFS, "../../scenarios/"+e.Name())
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
	return s, nil
}
```

- [ ] **Step 5: Implement `internal/promql/promql.go`**

```go
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
```

- [ ] **Step 6: Run tests to confirm pass**

```bash
go test ./internal/promql/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add scenarios internal/promql
git commit -m "feat(promql): load embedded scenario YAMLs and render with text/template"
```

---

## Task 7: `cmd/root.go` — cobra root command + global flags

**Files:**
- Modify: `cmd/root.go` (replace stub from Task 1)
- Create: `cmd/version.go`

- [ ] **Step 1: Replace `cmd/root.go`**

```go
// Package cmd wires the cobra command tree.
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
)

var version = "dev"

// Globals populated by flag parsing on the root command.
type globalFlags struct {
	VMURL          string
	Cluster        string
	BasicAuthUser  string
	BasicAuthPass  string
	Timeout        time.Duration
	Format         string
	DryRun         bool
}

var globals globalFlags

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "hbase-metrics-cli",
		Short: "Diagnose HBase clusters via VictoriaMetrics — designed for Claude Code.",
		Long: `hbase-metrics-cli runs predefined diagnostic scenarios against a
VictoriaMetrics endpoint and emits structured JSON / table / markdown so
Claude Code (and other AI agents) can analyze HBase health.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&globals.VMURL, "vm-url", "", "VictoriaMetrics base URL (overrides config / HBASE_VM_URL)")
	root.PersistentFlags().StringVar(&globals.Cluster, "cluster", "", "Cluster label value (overrides default_cluster)")
	root.PersistentFlags().StringVar(&globals.BasicAuthUser, "basic-auth-user", "", "Basic Auth username (overrides HBASE_VM_USER)")
	root.PersistentFlags().StringVar(&globals.BasicAuthPass, "basic-auth-pass", "", "Basic Auth password (overrides HBASE_VM_PASS)")
	root.PersistentFlags().DurationVar(&globals.Timeout, "timeout", 0, "HTTP timeout (e.g. 10s)")
	root.PersistentFlags().StringVar(&globals.Format, "format", "json", "Output format: json | table | markdown")
	root.PersistentFlags().BoolVar(&globals.DryRun, "dry-run", false, "Print rendered PromQL without calling VictoriaMetrics")
	return root
}

// LoadEffectiveConfig merges flag/env/file/default and validates.
func LoadEffectiveConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	config.ApplyEnv(cfg)
	config.ApplyFlags(cfg, config.FlagOverrides{
		VMURL:          globals.VMURL,
		DefaultCluster: globals.Cluster,
		BasicAuthUser:  globals.BasicAuthUser,
		BasicAuthPass:  globals.BasicAuthPass,
		Timeout:        globals.Timeout,
	})
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Execute is the package entry point invoked by main.go.
func Execute() int {
	root := newRootCmd()
	register(root) // wires version, scenarios, query, config
	if err := root.Execute(); err != nil {
		cerrors.WriteJSON(os.Stderr, err)
		return cerrors.ExitCode(err)
	}
	return cerrors.ExitOK
}

// register is implemented in init.go to keep newRootCmd minimal.
func register(root *cobra.Command) {
	root.AddCommand(newVersionCmd())
	// scenarios.Register(root) added in Task 8
	// query.Register(root) added in Task 9
	// configcmd.Register(root) added in Task 9
	_ = fmt.Sprint // placeholder to keep imports stable across tasks
}
```

- [ ] **Step 2: Write `cmd/version.go`**

```go
package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build and runtime version info",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "hbase-metrics-cli %s (go %s)\n", version, runtime.Version())
			return nil
		},
	}
}
```

- [ ] **Step 3: Verify build and `version` command**

```bash
go build ./...
go run . version
```

Expected: prints `hbase-metrics-cli dev (go go1.23.x)`.

- [ ] **Step 4: Commit**

```bash
git add cmd/root.go cmd/version.go
git commit -m "feat(cmd): cobra root with global flags + version subcommand"
```

---

## Task 8: `cmd/scenarios` — auto-register scenario commands

**Files:**
- Create: `cmd/scenarios/runner.go`
- Create: `cmd/scenarios/register.go`
- Create: `cmd/scenarios/runner_test.go`
- Modify: `cmd/root.go` (replace `register` body)

- [ ] **Step 1: Write the failing test**

`cmd/scenarios/runner_test.go`:

```go
package scenarios

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

func TestRun_DryRunSkipsHTTPAndEmitsExprs(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()

	s := promql.Scenario{
		Name:    "demo",
		Range:   false,
		Queries: []promql.Query{{Label: "x", Expr: `up{cluster="{{.cluster}}"}`}},
		Columns: []string{"label", "value"},
	}
	out, err := Run(context.Background(), Inputs{
		Scenario: s,
		Vars:     promql.Vars{"cluster": "c1"},
		Client:   vmclient.New(vmclient.Options{BaseURL: srv.URL, Timeout: time.Second}),
		DryRun:   true,
	})
	require.NoError(t, err)
	require.Equal(t, 0, hits)
	require.Equal(t, `up{cluster="c1"}`, out.Queries[0].Expr)
	require.Empty(t, out.Data)
}

func TestRun_InstantQueryPopulatesData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"a"},"value":[1714291200,"1.5"]}]}}`))
	}))
	defer srv.Close()

	s := promql.Scenario{
		Name:    "demo",
		Range:   false,
		Queries: []promql.Query{{Label: "x", Expr: `up`}},
	}
	out, err := Run(context.Background(), Inputs{
		Scenario: s,
		Client:   vmclient.New(vmclient.Options{BaseURL: srv.URL, Timeout: time.Second}),
	})
	require.NoError(t, err)
	require.Len(t, out.Data, 1)
	require.Equal(t, "a", out.Data[0]["instance"])
	require.InDelta(t, 1.5, out.Data[0]["x"], 1e-9)
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./cmd/scenarios/...
```

Expected: compile errors.

- [ ] **Step 3: Implement `cmd/scenarios/runner.go`**

```go
// Package scenarios runs predefined HBase metric scenarios against VictoriaMetrics.
package scenarios

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

type Inputs struct {
	Scenario promql.Scenario
	Vars     promql.Vars
	Client   *vmclient.Client
	DryRun   bool
	// Range params (only used when Scenario.Range is true)
	End  time.Time
	Step time.Duration
	Since time.Duration
}

func Run(ctx context.Context, in Inputs) (output.Envelope, error) {
	rendered, err := promql.Render(in.Scenario, in.Vars)
	if err != nil {
		return output.Envelope{}, cerrors.Errorf(cerrors.CodeFlagInvalid, "render scenario %s: %v", in.Scenario.Name, err)
	}
	cluster, _ := in.Vars["cluster"].(string)
	env := output.Envelope{
		Scenario: in.Scenario.Name,
		Cluster:  cluster,
		Queries:  make([]output.Query, len(rendered)),
		Columns:  in.Scenario.Columns,
	}
	for i, r := range rendered {
		env.Queries[i] = output.Query{Label: r.Label, Expr: r.Expr}
	}
	if in.Scenario.Range {
		end := in.End
		if end.IsZero() {
			end = time.Now()
		}
		start := end.Add(-in.Since)
		env.Range = &output.Range{
			Start: start.UTC().Format(time.RFC3339),
			End:   end.UTC().Format(time.RFC3339),
			Step:  in.Step.String(),
		}
	}
	if in.DryRun {
		return env, nil
	}

	results := make([]vmclient.Result, len(rendered))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for i, r := range rendered {
		i, r := i, r
		g.Go(func() error {
			var (
				res *vmclient.Result
				err error
			)
			if in.Scenario.Range {
				end := in.End
				if end.IsZero() {
					end = time.Now()
				}
				res, err = in.Client.QueryRange(gctx, r.Expr, end.Add(-in.Since), end, in.Step)
			} else {
				res, err = in.Client.Query(gctx, r.Expr, time.Now())
			}
			if err != nil {
				return err
			}
			results[i] = *res
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return output.Envelope{}, err
	}

	env.Data = mergeByInstance(rendered, results, in.Scenario.Range)
	if len(env.Data) == 0 {
		return env, cerrors.WithHint(
			cerrors.Errorf(cerrors.CodeNoData, "scenario %s returned no data", in.Scenario.Name),
			"verify --cluster matches an active cluster label and --since covers a period with traffic",
		)
	}
	return env, nil
}

// mergeByInstance turns N parallel query results into one row per instance,
// with the query Label as the column key.
func mergeByInstance(rendered []promql.Rendered, results []vmclient.Result, isRange bool) []output.Row {
	rows := map[string]output.Row{}
	for i, res := range results {
		label := rendered[i].Label
		for _, sample := range res.Result {
			key := sample.Metric["instance"]
			if key == "" {
				key = fmt.Sprintf("%s/%d", label, i)
			}
			row, ok := rows[key]
			if !ok {
				row = output.Row{"instance": key}
				rows[key] = row
			}
			if isRange {
				row[label] = sample.Values
			} else {
				row[label] = parseFloat(sample.Value)
			}
		}
	}
	out := make([]output.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}
	return out
}

func parseFloat(v []any) any {
	if len(v) < 2 {
		return nil
	}
	if s, ok := v[1].(string); ok {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return v[1]
}
```

- [ ] **Step 4: Implement `cmd/scenarios/register.go`**

```go
package scenarios

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

// LoadConfigFn is wired by the cmd package to break an import cycle.
type LoadConfigFn func() (*config.Config, error)

// FormatFn returns the global --format value.
type FormatFn func() string

// DryRunFn returns the global --dry-run flag.
type DryRunFn func() bool

func Register(root *cobra.Command, loadCfg LoadConfigFn, format FormatFn, dryRun DryRunFn) error {
	scenarios, err := promql.LoadEmbedded()
	if err != nil {
		return err
	}
	for _, s := range scenarios {
		root.AddCommand(buildCmd(s, loadCfg, format, dryRun))
	}
	return nil
}

func buildCmd(s promql.Scenario, loadCfg LoadConfigFn, format FormatFn, dryRun DryRunFn) *cobra.Command {
	flagValues := map[string]*string{}
	intValues := map[string]*int{}
	since := "5m"
	step := "30s"

	cmd := &cobra.Command{
		Use:   s.Name,
		Short: s.Description,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			vars := promql.Vars{"cluster": cfg.DefaultCluster}
			for k, v := range s.Defaults {
				vars[k] = v
			}
			for name, p := range flagValues {
				if *p != "" {
					vars[name] = *p
				}
			}
			for name, p := range intValues {
				vars[name] = *p
			}

			sinceDur, err := time.ParseDuration(since)
			if err != nil {
				return cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --since %q: %v", since, err)
			}
			stepDur, err := time.ParseDuration(step)
			if err != nil {
				return cerrors.Errorf(cerrors.CodeFlagInvalid, "invalid --step %q: %v", step, err)
			}
			vars["since"] = since
			vars["step"] = step
			vars["since_seconds"] = strconv.Itoa(int(sinceDur.Seconds()))

			client := vmclient.New(vmclient.Options{
				BaseURL:       cfg.VMURL,
				Timeout:       cfg.Timeout,
				BasicAuthUser: cfg.BasicAuth.Username,
				BasicAuthPass: cfg.BasicAuth.Password,
			})

			env, err := Run(cmd.Context(), Inputs{
				Scenario: s,
				Vars:     vars,
				Client:   client,
				DryRun:   dryRun(),
				Since:    sinceDur,
				Step:     stepDur,
			})
			if err != nil && !isNoData(err) {
				return err
			}
			if err := output.Render(format(), env, cmd.OutOrStdout()); err != nil {
				return err
			}
			if err != nil { // NoData warning -> stderr, exit 0
				cerrors.WriteJSON(os.Stderr, err)
			}
			return nil
		},
	}

	for _, f := range s.Flags {
		switch f.Type {
		case "int":
			def := 0
			if v, ok := f.Default.(int); ok {
				def = v
			}
			p := new(int)
			*p = def
			cmd.Flags().IntVar(p, f.Name, def, fmt.Sprintf("%s (default %d)", f.Help, def))
			intValues[f.Name] = p
		default:
			def := ""
			if v, ok := f.Default.(string); ok {
				def = v
			}
			p := new(string)
			*p = def
			cmd.Flags().StringVar(p, f.Name, def, f.Help)
			flagValues[f.Name] = p
		}
	}

	if s.Range {
		cmd.Flags().StringVar(&since, "since", "5m", "Range duration (e.g. 5m, 1h)")
		cmd.Flags().StringVar(&step, "step", "30s", "Range step (e.g. 30s)")
	}
	cmd.Flags().Bool("range", s.Range, "")
	_ = cmd.Flags().MarkHidden("range")
	return cmd
}

func isNoData(err error) bool {
	var ce *cerrors.CodedError
	if !errorsAs(err, &ce) {
		return false
	}
	return ce.Code == cerrors.CodeNoData
}

// errorsAs is a tiny wrapper to keep imports tidy.
func errorsAs(err error, target **cerrors.CodedError) bool {
	if err == nil {
		return false
	}
	if ce, ok := err.(*cerrors.CodedError); ok {
		*target = ce
		return true
	}
	return false
}

// Avoid unused import warning before context is used in any of the helpers above.
var _ = context.Background
```

- [ ] **Step 5: Update `cmd/root.go` `register` to wire scenarios**

Replace the `register` function body in `cmd/root.go`:

```go
func register(root *cobra.Command) {
	root.AddCommand(newVersionCmd())
	if err := scenarios.Register(root, LoadEffectiveConfig, func() string { return globals.Format }, func() bool { return globals.DryRun }); err != nil {
		fmt.Fprintf(os.Stderr, "scenario registration failed: %v\n", err)
		os.Exit(cerrors.ExitInternal)
	}
	// query.Register(root) added in Task 9
	// configcmd.Register(root) added in Task 9
}
```

Add the import:

```go
import (
	// ... existing
	"github.com/opay-bigdata/hbase-metrics-cli/cmd/scenarios"
)
```

- [ ] **Step 6: Run tests**

```bash
go test ./...
go build ./...
```

Expected: PASS, build succeeds (12 scenarios will be added in Task 10).

- [ ] **Step 7: Commit**

```bash
git add cmd/scenarios cmd/root.go
git commit -m "feat(cmd): scenario runner + auto-registration from embed.FS"
```

---

## Task 9: `query` and `config` subcommands

**Files:**
- Create: `cmd/query.go`
- Create: `cmd/configcmd/configcmd.go`
- Create: `cmd/configcmd/init.go`
- Create: `cmd/configcmd/show.go`
- Modify: `cmd/root.go`

- [ ] **Step 1: Write `cmd/query.go`**

```go
package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	cerrors "github.com/opay-bigdata/hbase-metrics-cli/internal/errors"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

func newQueryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query <promql>",
		Short: "Run a raw PromQL instant query (escape hatch).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadEffectiveConfig()
			if err != nil {
				return err
			}
			client := vmclient.New(vmclient.Options{
				BaseURL:       cfg.VMURL,
				Timeout:       cfg.Timeout,
				BasicAuthUser: cfg.BasicAuth.Username,
				BasicAuthPass: cfg.BasicAuth.Password,
			})
			ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
			defer cancel()
			res, err := client.Query(ctx, args[0], time.Now())
			if err != nil {
				return err
			}
			env := output.Envelope{
				Scenario: "query",
				Cluster:  cfg.DefaultCluster,
				Queries:  []output.Query{{Label: "raw", Expr: args[0]}},
				Data:     []output.Row{},
			}
			for _, s := range res.Result {
				row := output.Row{"instance": s.Metric["instance"]}
				if len(s.Value) >= 2 {
					row["value"] = s.Value[1]
				}
				for k, v := range s.Metric {
					if k != "instance" {
						row[k] = v
					}
				}
				env.Data = append(env.Data, row)
			}
			env.Columns = []string{"instance", "value"}
			if len(env.Data) == 0 {
				return cerrors.WithHint(cerrors.Errorf(cerrors.CodeNoData, "query returned no data"), "verify the PromQL expression and label values")
			}
			return output.Render(globals.Format, env, cmd.OutOrStdout())
		},
	}
}
```

- [ ] **Step 2: Write `cmd/configcmd/configcmd.go`**

```go
// Package configcmd hosts the `config` subcommands.
package configcmd

import "github.com/spf13/cobra"

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage hbase-metrics-cli configuration",
	}
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newShowCmd())
	return cmd
}
```

- [ ] **Step 3: Write `cmd/configcmd/init.go`**

```go
package configcmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactively create ~/.config/hbase-metrics-cli/config.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r := bufio.NewReader(cmd.InOrStdin())
			cfg := &config.Config{Timeout: 10 * time.Second}
			cfg.VMURL = prompt(r, cmd.OutOrStdout(), "VictoriaMetrics URL", "https://vm.example.com/")
			cfg.DefaultCluster = prompt(r, cmd.OutOrStdout(), "Default cluster label", "")
			cfg.BasicAuth.Username = prompt(r, cmd.OutOrStdout(), "Basic Auth username (blank to skip)", "")
			if cfg.BasicAuth.Username != "" {
				cfg.BasicAuth.Password = prompt(r, cmd.OutOrStdout(), "Basic Auth password", "")
			}
			to := prompt(r, cmd.OutOrStdout(), "HTTP timeout", "10s")
			d, err := time.ParseDuration(to)
			if err != nil {
				return fmt.Errorf("invalid timeout %q: %w", to, err)
			}
			cfg.Timeout = d
			if err := config.Save(cfg); err != nil {
				return err
			}
			path, _ := config.ConfigPath()
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
			return nil
		},
	}
}

func prompt(r *bufio.Reader, w *os.File, label, def string) string {
	fmt.Fprintf(w, "%s [%s]: ", label, def)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}
```

Note: `cmd.OutOrStdout()` returns `io.Writer`, not `*os.File` — adjust signature:

Replace `prompt` signature with:

```go
func prompt(r *bufio.Reader, w interface{ Write([]byte) (int, error) }, label, def string) string {
	fmt.Fprintf(w.(interface{ Write([]byte) (int, error) }), "%s [%s]: ", label, def)
```

Simpler — use `io.Writer`:

```go
import "io"

func prompt(r *bufio.Reader, w io.Writer, label, def string) string {
	fmt.Fprintf(w, "%s [%s]: ", label, def)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}
```

- [ ] **Step 4: Write `cmd/configcmd/show.go`**

```go
package configcmd

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/config"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print effective configuration with sources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			config.ApplyEnv(cfg)
			path, _ := config.ConfigPath()
			out := map[string]any{
				"path":            path,
				"vm_url":          cfg.VMURL,
				"default_cluster": cfg.DefaultCluster,
				"basic_auth_set":  cfg.BasicAuth.Username != "" || cfg.BasicAuth.Password != "",
				"timeout":         cfg.Timeout.String(),
				"sources": map[string]string{
					"vm_url":          string(cfg.Source.VMURL),
					"default_cluster": string(cfg.Source.DefaultCluster),
					"basic_auth":      string(cfg.Source.BasicAuth),
					"timeout":         string(cfg.Source.Timeout),
				},
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
}
```

- [ ] **Step 5: Wire into `cmd/root.go` `register`**

Update `register` body:

```go
func register(root *cobra.Command) {
	root.AddCommand(newVersionCmd())
	root.AddCommand(newQueryCmd())
	root.AddCommand(configcmd.New())
	if err := scenarios.Register(root, LoadEffectiveConfig, func() string { return globals.Format }, func() bool { return globals.DryRun }); err != nil {
		fmt.Fprintf(os.Stderr, "scenario registration failed: %v\n", err)
		os.Exit(cerrors.ExitInternal)
	}
}
```

Add import: `"github.com/opay-bigdata/hbase-metrics-cli/cmd/configcmd"`.

- [ ] **Step 6: Smoke test**

```bash
go build ./...
go run . --help
go run . config show
go run . version
```

Expected: help shows `config`, `query`, `version` subcommands; `config show` prints JSON with `default` sources.

- [ ] **Step 7: Commit**

```bash
git add cmd
git commit -m "feat(cmd): add query escape hatch and config init/show subcommands"
```

---

## Task 10: Scenario batch A — cluster-level (3 scenarios)

**Files:**
- Create: `scenarios/cluster-overview.yaml`
- Create: `scenarios/regionserver-list.yaml`
- Create: `scenarios/requests-qps.yaml`
- Create: `tests/golden/cluster-overview.golden.promql`
- Create: `tests/golden/regionserver-list.golden.promql`
- Create: `tests/golden/requests-qps.golden.promql`
- Create: `tests/golden/golden_test.go` (covers all 12; updated each batch)

**Note on metric names:** these YAML templates assume the `jmx_hbase.yaml` rule output (`hadoop_hbase_<lower>` with labels `role`, `sub`, `cluster`, `instance`). The implementer may need to verify exact attribute names against `https://vm.rupiahcepatweb.com/api/v1/labels?match[]=hadoop_hbase_*` once the cluster is reachable; adjust the template strings only — no Go code change needed.

- [ ] **Step 1: Write `scenarios/cluster-overview.yaml`**

```yaml
name: cluster-overview
description: Cluster-wide HBase health summary (RS count, regions, requests, RPC P99).
range: false
columns: [label, value]
queries:
  - label: regionserver_count
    expr: |
      count(hadoop_hbase_numregionservers{cluster="{{.cluster}}", role="Master", sub="Server"})
  - label: dead_regionserver_count
    expr: |
      max(hadoop_hbase_numdeadregionservers{cluster="{{.cluster}}", role="Master", sub="Server"})
  - label: total_regions
    expr: |
      sum(hadoop_hbase_regioncount{cluster="{{.cluster}}", role="RegionServer", sub="Server"})
  - label: total_request_qps
    expr: |
      sum(rate(hadoop_hbase_totalrequestcount{cluster="{{.cluster}}", role="RegionServer", sub="Server"}[1m]))
  - label: rpc_processcalltime_p99_max
    expr: |
      max(hadoop_hbase_ipc_processcalltime_99th_percentile{cluster="{{.cluster}}", role="RegionServer"})
```

- [ ] **Step 2: Write `scenarios/regionserver-list.yaml`**

```yaml
name: regionserver-list
description: Per-RegionServer region count, request QPS, and heap usage.
range: false
columns: [instance, regions, qps, heap_used_mb]
queries:
  - label: regions
    expr: |
      hadoop_hbase_regioncount{cluster="{{.cluster}}", role="RegionServer", sub="Server"}
  - label: qps
    expr: |
      sum by (instance) (rate(hadoop_hbase_totalrequestcount{cluster="{{.cluster}}", role="RegionServer", sub="Server"}[1m]))
  - label: heap_used_mb
    expr: |
      hadoop_hbase_memheapusedm{cluster="{{.cluster}}", role="RegionServer", sub="JvmMetrics"}
```

- [ ] **Step 3: Write `scenarios/requests-qps.yaml`**

```yaml
name: requests-qps
description: Read / write / total request QPS across the cluster.
range: true
defaults:
  since: 10m
  step: 30s
columns: [instance, read_qps, write_qps, total_qps]
queries:
  - label: read_qps
    expr: |
      sum by (instance) (rate(hadoop_hbase_readrequestcount{cluster="{{.cluster}}", role="RegionServer", sub="Server"}[1m]))
  - label: write_qps
    expr: |
      sum by (instance) (rate(hadoop_hbase_writerequestcount{cluster="{{.cluster}}", role="RegionServer", sub="Server"}[1m]))
  - label: total_qps
    expr: |
      sum by (instance) (rate(hadoop_hbase_totalrequestcount{cluster="{{.cluster}}", role="RegionServer", sub="Server"}[1m]))
```

- [ ] **Step 4: Write golden test runner `tests/golden/golden_test.go`**

```go
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
```

- [ ] **Step 5: Generate golden files for the three batch-A scenarios**

```bash
go test -run TestRender_Goldens -update ./tests/golden/...
```

Expected: writes `cluster-overview.golden.promql`, `regionserver-list.golden.promql`, `requests-qps.golden.promql`. (Other scenarios will be added in subsequent batches; the test will pass with all current scenarios.)

- [ ] **Step 6: Run tests**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add scenarios tests/golden
git commit -m "feat(scenarios): add cluster-overview, regionserver-list, requests-qps"
```

---

## Task 11: Scenario batch B — RPC & queue (3 scenarios)

**Files:**
- Create: `scenarios/rpc-latency.yaml`
- Create: `scenarios/handler-queue.yaml`
- Create: `scenarios/hotspot-detect.yaml`
- Generate: `tests/golden/rpc-latency.golden.promql` and two more

- [ ] **Step 1: Write `scenarios/rpc-latency.yaml`**

```yaml
name: rpc-latency
description: HBase RegionServer / Master RPC P99 / P999 latency (ms), top-K instances.
range: true
defaults:
  since: 10m
  step: 30s
flags:
  - name: role
    type: string
    default: RegionServer
    enum: [Master, RegionServer]
    help: Role label value (Master | RegionServer)
  - name: top
    type: int
    default: 10
    help: Top-K instances by P99
columns: [instance, p99, p999]
queries:
  - label: p99
    expr: |
      topk({{.top}}, hadoop_hbase_ipc_processcalltime_99th_percentile{cluster="{{.cluster}}", role="{{.role}}"})
  - label: p999
    expr: |
      topk({{.top}}, hadoop_hbase_ipc_processcalltime_999th_percentile{cluster="{{.cluster}}", role="{{.role}}"})
```

- [ ] **Step 2: Write `scenarios/handler-queue.yaml`**

```yaml
name: handler-queue
description: IPC handler queue length and active handler count per RegionServer.
range: false
columns: [instance, general_queue, priority_queue, active_handlers]
queries:
  - label: general_queue
    expr: |
      hadoop_hbase_ipc_numcallsingeneralqueue{cluster="{{.cluster}}", role="RegionServer"}
  - label: priority_queue
    expr: |
      hadoop_hbase_ipc_numcallsinpriorityqueue{cluster="{{.cluster}}", role="RegionServer"}
  - label: active_handlers
    expr: |
      hadoop_hbase_ipc_numactivehandler{cluster="{{.cluster}}", role="RegionServer"}
```

- [ ] **Step 3: Write `scenarios/hotspot-detect.yaml`**

```yaml
name: hotspot-detect
description: Top-K hot RegionServers by request QPS and active-handler ratio.
range: false
flags:
  - name: top
    type: int
    default: 5
    help: Top-K hottest RegionServers
columns: [instance, qps, active_handlers]
queries:
  - label: qps
    expr: |
      topk({{.top}}, sum by (instance) (rate(hadoop_hbase_totalrequestcount{cluster="{{.cluster}}", role="RegionServer", sub="Server"}[1m])))
  - label: active_handlers
    expr: |
      topk({{.top}}, hadoop_hbase_ipc_numactivehandler{cluster="{{.cluster}}", role="RegionServer"})
```

- [ ] **Step 4: Generate goldens for batch B**

```bash
go test -run TestRender_Goldens -update ./tests/golden/...
go test ./tests/golden/...
```

Expected: 6 scenarios pass.

- [ ] **Step 5: Smoke test --dry-run**

```bash
go run . rpc-latency --vm-url http://localhost:9090 --cluster c1 --dry-run
```

Expected: stdout JSON with rendered `topk(10, ...)` PromQL, no HTTP attempt.

- [ ] **Step 6: Commit**

```bash
git add scenarios tests/golden
git commit -m "feat(scenarios): add rpc-latency, handler-queue, hotspot-detect"
```

---

## Task 12: Scenario batch C — JVM (2 scenarios)

**Files:**
- Create: `scenarios/gc-pressure.yaml`
- Create: `scenarios/jvm-memory.yaml`
- Generate goldens

- [ ] **Step 1: Write `scenarios/gc-pressure.yaml`**

```yaml
name: gc-pressure
description: GC count and elapsed time per RegionServer (per-collector).
range: true
defaults:
  since: 15m
  step: 1m
columns: [instance, gc_count_per_min, gc_time_ms_per_min]
queries:
  - label: gc_count_per_min
    expr: |
      sum by (instance) (rate(jvm_gc_collectioncount{cluster="{{.cluster}}", role="RegionServer"}[5m])) * 60
  - label: gc_time_ms_per_min
    expr: |
      sum by (instance) (rate(jvm_gc_collectiontime{cluster="{{.cluster}}", role="RegionServer"}[5m])) * 60
```

- [ ] **Step 2: Write `scenarios/jvm-memory.yaml`**

```yaml
name: jvm-memory
description: JVM heap and non-heap usage per RegionServer.
range: false
columns: [instance, heap_used_mb, heap_max_mb, heap_used_pct]
queries:
  - label: heap_used_mb
    expr: |
      hadoop_hbase_memheapusedm{cluster="{{.cluster}}", role="RegionServer", sub="JvmMetrics"}
  - label: heap_max_mb
    expr: |
      hadoop_hbase_memheapmaxm{cluster="{{.cluster}}", role="RegionServer", sub="JvmMetrics"}
  - label: heap_used_pct
    expr: |
      100 * hadoop_hbase_memheapusedm{cluster="{{.cluster}}", role="RegionServer", sub="JvmMetrics"}
        / hadoop_hbase_memheapmaxm{cluster="{{.cluster}}", role="RegionServer", sub="JvmMetrics"}
```

- [ ] **Step 3: Generate goldens**

```bash
go test -run TestRender_Goldens -update ./tests/golden/...
go test ./tests/golden/...
```

- [ ] **Step 4: Commit**

```bash
git add scenarios tests/golden
git commit -m "feat(scenarios): add gc-pressure, jvm-memory"
```

---

## Task 13: Scenario batch D — storage layer (3 scenarios)

**Files:**
- Create: `scenarios/compaction-status.yaml`
- Create: `scenarios/blockcache-hitrate.yaml`
- Create: `scenarios/wal-stats.yaml`
- Generate goldens

- [ ] **Step 1: Write `scenarios/compaction-status.yaml`**

```yaml
name: compaction-status
description: Compaction queue length and minor/major compactions per RegionServer.
range: true
defaults:
  since: 30m
  step: 1m
columns: [instance, compaction_queue, compacted_cells_qps]
queries:
  - label: compaction_queue
    expr: |
      hadoop_hbase_compactionqueuelength{cluster="{{.cluster}}", role="RegionServer", sub="Server"}
  - label: compacted_cells_qps
    expr: |
      sum by (instance) (rate(hadoop_hbase_compactedcellscount{cluster="{{.cluster}}", role="RegionServer", sub="Server"}[5m]))
```

- [ ] **Step 2: Write `scenarios/blockcache-hitrate.yaml`**

```yaml
name: blockcache-hitrate
description: BlockCache hit ratio and size per RegionServer.
range: false
columns: [instance, hit_ratio_pct, size_mb, evicted_qps]
queries:
  - label: hit_ratio_pct
    expr: |
      hadoop_hbase_blockcachehitcachingratio{cluster="{{.cluster}}", role="RegionServer", sub="Server"}
  - label: size_mb
    expr: |
      hadoop_hbase_blockcachesize{cluster="{{.cluster}}", role="RegionServer", sub="Server"} / 1024 / 1024
  - label: evicted_qps
    expr: |
      sum by (instance) (rate(hadoop_hbase_blockcacheevictedcount{cluster="{{.cluster}}", role="RegionServer", sub="Server"}[5m]))
```

- [ ] **Step 3: Write `scenarios/wal-stats.yaml`**

```yaml
name: wal-stats
description: WAL append count, sync time P99, and slow append count per RegionServer.
range: true
defaults:
  since: 15m
  step: 30s
columns: [instance, append_qps, sync_p99_ms, slow_appends_per_min]
queries:
  - label: append_qps
    expr: |
      sum by (instance) (rate(hadoop_hbase_appendcount{cluster="{{.cluster}}", role="RegionServer", sub="WAL"}[1m]))
  - label: sync_p99_ms
    expr: |
      hadoop_hbase_synctime_99th_percentile{cluster="{{.cluster}}", role="RegionServer", sub="WAL"}
  - label: slow_appends_per_min
    expr: |
      sum by (instance) (rate(hadoop_hbase_slowappendcount{cluster="{{.cluster}}", role="RegionServer", sub="WAL"}[5m])) * 60
```

- [ ] **Step 4: Generate goldens & commit**

```bash
go test -run TestRender_Goldens -update ./tests/golden/...
go test ./tests/golden/...
git add scenarios tests/golden
git commit -m "feat(scenarios): add compaction-status, blockcache-hitrate, wal-stats"
```

---

## Task 14: Scenario batch E — master state (1 scenario)

**Files:**
- Create: `scenarios/master-status.yaml`
- Generate golden

- [ ] **Step 1: Write `scenarios/master-status.yaml`**

```yaml
name: master-status
description: HBase Master state — average load, RIT count, balancer status.
range: false
columns: [label, value]
queries:
  - label: regionservers_active
    expr: |
      hadoop_hbase_numregionservers{cluster="{{.cluster}}", role="Master", sub="Server"}
  - label: regionservers_dead
    expr: |
      hadoop_hbase_numdeadregionservers{cluster="{{.cluster}}", role="Master", sub="Server"}
  - label: average_load
    expr: |
      hadoop_hbase_averageload{cluster="{{.cluster}}", role="Master", sub="Server"}
  - label: regions_in_transition
    expr: |
      hadoop_hbase_ritcount{cluster="{{.cluster}}", role="Master", sub="AssignmentManager"}
  - label: rit_oldest_age_ms
    expr: |
      hadoop_hbase_ritoldestage{cluster="{{.cluster}}", role="Master", sub="AssignmentManager"}
```

- [ ] **Step 2: Generate golden, run all tests, commit**

```bash
go test -run TestRender_Goldens -update ./tests/golden/...
go test ./...
git add scenarios tests/golden
git commit -m "feat(scenarios): add master-status (12/12 scenarios complete)"
```

Expected: all 12 scenarios load via `LoadEmbedded`, golden test passes.

---

## Task 15: e2e dry-run tests for all 12 scenarios

**Files:**
- Create: `tests/e2e/dryrun_test.go`

- [ ] **Step 1: Write `tests/e2e/dryrun_test.go`**

```go
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
```

- [ ] **Step 2: Build binary and run e2e**

```bash
make build
go test -tags=e2e ./tests/e2e/...
```

Expected: PASS for all 12 scenarios.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e
git commit -m "test(e2e): add dry-run e2e for all 12 scenarios"
```

---

## Task 16: GoReleaser distribution config

**Files:**
- Create: `.goreleaser.yml`

- [ ] **Step 1: Write `.goreleaser.yml`**

```yaml
version: 2
project_name: hbase-metrics-cli
builds:
  - id: hbase-metrics-cli
    main: ./
    binary: hbase-metrics-cli
    env: [CGO_ENABLED=0]
    goos: [darwin, linux]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X github.com/opay-bigdata/hbase-metrics-cli/cmd.version={{.Version}}
archives:
  - id: default
    formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - README.md
      - README.zh.md
      - LICENSE
      - .claude/skills/hbase-metrics/SKILL.md
checksum:
  name_template: "checksums.txt"
release:
  draft: true
```

- [ ] **Step 2: Smoke-test snapshot build**

```bash
make release-snapshot
ls dist/
```

Expected: 4 binary archives + checksums.

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yml
git commit -m "build: add goreleaser config for darwin/linux × amd64/arm64"
```

---

## Task 17: English README

**Files:**
- Create: `LICENSE` (MIT)
- Create: `README.md`

- [ ] **Step 1: Write `LICENSE`**

Standard MIT — copy from <https://opensource.org/license/mit>, set copyright holder to "OPay Bigdata" and year 2026.

- [ ] **Step 2: Write `README.md`**

```markdown
# hbase-metrics-cli

[中文版](./README.zh.md) | [English](./README.md)

A Go CLI for diagnosing HBase clusters via VictoriaMetrics — built for **Claude Code** and other AI agents. Twelve predefined scenarios output structured JSON; humans can also use `--format table|markdown`.

## Why

- **Agent-Native** — flat 12 commands map 1:1 to common HBase diagnostic questions; output JSON envelope includes the rendered PromQL so the agent can drill in.
- **Single binary** — no Node, no Python; `go install` or download a release.
- **YAML scenarios** — PromQL templates live in `scenarios/*.yaml`, easy to fork and adapt.

## Install

### From source

```bash
go install github.com/opay-bigdata/hbase-metrics-cli@latest
```

### From release

Download the archive for your OS from GitHub Releases, then `tar -xzf … && mv hbase-metrics-cli /usr/local/bin/`.

## Quick Start (Human)

```bash
# 1. Configure (interactive)
hbase-metrics-cli config init

# 2. Check
hbase-metrics-cli config show

# 3. Diagnose
hbase-metrics-cli cluster-overview --format table
```

## Quick Start (AI Agent / Claude Code)

```bash
# 1. Install binary
go install github.com/opay-bigdata/hbase-metrics-cli@latest

# 2. Pre-fill config (non-interactive)
mkdir -p ~/.config/hbase-metrics-cli
cat > ~/.config/hbase-metrics-cli/config.yaml <<EOF
vm_url: https://vm.example.com/
default_cluster: mrs-hbase-oline
timeout: 10s
EOF

# 3. Install Skill
ln -sf "$(pwd)/.claude/skills/hbase-metrics" ~/.claude/skills/hbase-metrics

# 4. Smoke
hbase-metrics-cli cluster-overview --format json
```

## Scenarios (12)

| Command | Use when |
|---|---|
| `cluster-overview` | "How is the cluster overall?" |
| `regionserver-list` | "Which RS holds how many regions / what QPS?" |
| `requests-qps` | "Read vs write QPS over time?" |
| `rpc-latency` | "RPC P99 / P999 — top offenders?" |
| `handler-queue` | "IPC handlers backed up?" |
| `hotspot-detect` | "Single-RS hotspot?" |
| `gc-pressure` | "GC frequency / pause too high?" |
| `jvm-memory` | "Heap usage trend?" |
| `compaction-status` | "Compaction backlog?" |
| `blockcache-hitrate` | "BlockCache effective?" |
| `wal-stats` | "WAL slow appends / sync latency?" |
| `master-status` | "Master state, RIT count, average load?" |
| `query '<promql>'` | Escape hatch for raw PromQL |

## Common Flags

| Flag | Default | Notes |
|---|---|---|
| `--vm-url` | from config | Overrides `HBASE_VM_URL` |
| `--cluster` | from config | Cluster label value |
| `--basic-auth-user` / `--basic-auth-pass` | empty | Or `HBASE_VM_USER` / `HBASE_VM_PASS` |
| `--timeout` | `10s` | HTTP timeout |
| `--format` | `json` | `json` / `table` / `markdown` |
| `--dry-run` | `false` | Print rendered PromQL, skip HTTP |

Per-scenario flags are listed via `hbase-metrics-cli <scenario> --help`.

## Output Contract

**stdout** — JSON envelope (`--format json`):

```json
{
  "scenario": "rpc-latency",
  "cluster": "mrs-hbase-oline",
  "range": {"start": "...", "end": "...", "step": "30s"},
  "queries": [{"label": "p99", "expr": "topk(10, ...)"}],
  "columns": ["instance", "p99", "p999"],
  "data": [{"instance": "10.0.0.1:19110", "p99": 12.3, "p999": 25.1}]
}
```

**stderr** — structured errors:

```json
{"error": {"code": "VM_HTTP_4XX", "message": "...", "hint": "..."}}
```

**Exit codes:** `0` success / NoData warning · `1` internal · `2` user error · `3` VM failure.

## Configuration

`~/.config/hbase-metrics-cli/config.yaml` (also via `$HBASE_METRICS_CLI_CONFIG_DIR`):

```yaml
vm_url: https://vm.example.com/
default_cluster: mrs-hbase-oline
basic_auth:
  username: ""
  password: ""
timeout: 10s
```

Resolution order (highest wins): `flag` > `env` > `config.yaml` > built-in default.

## Development

```bash
make unit-test     # go test -race ./...
make e2e-dry       # all 12 scenarios in --dry-run mode
make lint          # vet + gofmt + golangci-lint
make build         # produce ./hbase-metrics-cli
```

## License

MIT.
```

- [ ] **Step 3: Commit**

```bash
git add LICENSE README.md
git commit -m "docs: add MIT license and English README"
```

---

## Task 18: Chinese README

**Files:**
- Create: `README.zh.md`

- [ ] **Step 1: Write `README.zh.md`**

```markdown
# hbase-metrics-cli

[中文版](./README.zh.md) | [English](./README.md)

面向 **Claude Code** 等 AI Agent 的 HBase 监控诊断 CLI（Go 实现）—— 通过 VictoriaMetrics 查询 12 个预定义场景，默认输出结构化 JSON，方便 Agent 解析；人也可以 `--format table|markdown`。

## 为什么

- **面向 Agent** —— 12 个扁平命令 1:1 对应 HBase 常见诊断问题；JSON envelope 同时回吐渲染后的 PromQL，方便 Agent 钻取。
- **单二进制** —— 不依赖 Node / Python；`go install` 或下载 release。
- **场景即 YAML** —— PromQL 模板放在 `scenarios/*.yaml`，可 fork 改写。

## 安装

### 源码安装

```bash
go install github.com/opay-bigdata/hbase-metrics-cli@latest
```

### 二进制安装

从 GitHub Releases 下载对应操作系统的 archive，`tar -xzf … && mv hbase-metrics-cli /usr/local/bin/`。

## 快速上手（人类）

```bash
# 1. 配置（交互式）
hbase-metrics-cli config init

# 2. 校验
hbase-metrics-cli config show

# 3. 诊断
hbase-metrics-cli cluster-overview --format table
```

## 快速上手（AI Agent / Claude Code）

```bash
# 1. 安装二进制
go install github.com/opay-bigdata/hbase-metrics-cli@latest

# 2. 预置配置（非交互）
mkdir -p ~/.config/hbase-metrics-cli
cat > ~/.config/hbase-metrics-cli/config.yaml <<EOF
vm_url: https://vm.example.com/
default_cluster: mrs-hbase-oline
timeout: 10s
EOF

# 3. 安装 Claude Code Skill
ln -sf "$(pwd)/.claude/skills/hbase-metrics" ~/.claude/skills/hbase-metrics

# 4. 自检
hbase-metrics-cli cluster-overview --format json
```

## 12 个场景

| 命令 | 适用情境 |
|---|---|
| `cluster-overview` | 集群整体状态 |
| `regionserver-list` | 各 RS 的 region 数 / QPS / heap |
| `requests-qps` | 读 / 写 / 总 QPS 时间序列 |
| `rpc-latency` | RPC P99 / P999 Top-K |
| `handler-queue` | IPC handler 队列堆积 |
| `hotspot-detect` | 单 RS 热点检测 |
| `gc-pressure` | GC 频率 / 暂停 |
| `jvm-memory` | 堆使用率趋势 |
| `compaction-status` | compaction 队列堆积 |
| `blockcache-hitrate` | BlockCache 命中率 / 大小 |
| `wal-stats` | WAL 慢 append / sync P99 |
| `master-status` | Master 状态、RIT、平均负载 |
| `query '<promql>'` | 兜底原始 PromQL |

## 通用参数

| Flag | 默认 | 说明 |
|---|---|---|
| `--vm-url` | 来自 config | 覆盖 `HBASE_VM_URL` |
| `--cluster` | 来自 config | cluster 标签值 |
| `--basic-auth-user` / `--basic-auth-pass` | 空 | 或 `HBASE_VM_USER` / `HBASE_VM_PASS` |
| `--timeout` | `10s` | HTTP 超时 |
| `--format` | `json` | `json` / `table` / `markdown` |
| `--dry-run` | `false` | 仅打印渲染后的 PromQL，不发请求 |

各场景独有参数：`hbase-metrics-cli <scenario> --help`。

## 输出契约

**stdout** —— JSON envelope（`--format json`）：

```json
{
  "scenario": "rpc-latency",
  "cluster": "mrs-hbase-oline",
  "range": {"start": "...", "end": "...", "step": "30s"},
  "queries": [{"label": "p99", "expr": "topk(10, ...)"}],
  "columns": ["instance", "p99", "p999"],
  "data": [{"instance": "10.0.0.1:19110", "p99": 12.3, "p999": 25.1}]
}
```

**stderr** —— 结构化错误：

```json
{"error": {"code": "VM_HTTP_4XX", "message": "...", "hint": "..."}}
```

**退出码：** `0` 成功 / NoData 警告 · `1` 内部错 · `2` 用户错 · `3` VM 故障。

## 配置文件

`~/.config/hbase-metrics-cli/config.yaml`（也可通过 `$HBASE_METRICS_CLI_CONFIG_DIR` 自定义目录）：

```yaml
vm_url: https://vm.example.com/
default_cluster: mrs-hbase-oline
basic_auth:
  username: ""
  password: ""
timeout: 10s
```

覆盖优先级（高到低）：`flag` > `env` > `config.yaml` > 编译期默认。

## 开发

```bash
make unit-test     # go test -race ./...
make e2e-dry       # 12 个场景 dry-run e2e
make lint          # vet + gofmt + golangci-lint
make build         # 产出 ./hbase-metrics-cli
```

## License

MIT。
```

- [ ] **Step 2: Commit**

```bash
git add README.zh.md
git commit -m "docs: add Chinese README"
```

---

## Task 19: Claude Code Skill

**Files:**
- Create: `.claude/skills/hbase-metrics/SKILL.md`

- [ ] **Step 1: Write `.claude/skills/hbase-metrics/SKILL.md`**

```markdown
---
name: hbase-metrics
description: Use when diagnosing HBase cluster health/performance — RPC latency, GC, hotspots, compaction backlog, blockcache hit rate, WAL slowness, master state. Queries VictoriaMetrics via hbase-metrics-cli and returns structured JSON for analysis.
---

# HBase Metrics Skill

## When to use
- User asks about HBase being slow, RPC latency high, single-RS hotspot, GC pressure, compaction backlog, read/write skew, blockcache thrashing, WAL slow appends, or "show me HBase status".
- User wants a current health check on an HBase cluster.

## Pre-flight check (run first)
1. `hbase-metrics-cli config show` — confirm VM URL and default cluster.
2. If `vm_url` source is `default`, prompt the user to run `hbase-metrics-cli config init` (or set `HBASE_VM_URL`).

## Twelve scenarios

| Scenario | When to use | Example |
|---|---|---|
| `cluster-overview` | First glance | `hbase-metrics-cli cluster-overview` |
| `regionserver-list` | RS distribution | `hbase-metrics-cli regionserver-list --format table` |
| `requests-qps` | QPS trend | `hbase-metrics-cli requests-qps --since 30m` |
| `rpc-latency` | "RPC slow" | `hbase-metrics-cli rpc-latency --top 10 --since 15m` |
| `handler-queue` | "Stuck calls" | `hbase-metrics-cli handler-queue` |
| `hotspot-detect` | "One RS hot" | `hbase-metrics-cli hotspot-detect --top 5` |
| `gc-pressure` | "GC heavy" | `hbase-metrics-cli gc-pressure --since 30m` |
| `jvm-memory` | Heap close to max | `hbase-metrics-cli jvm-memory` |
| `compaction-status` | Compaction backlog | `hbase-metrics-cli compaction-status --since 1h` |
| `blockcache-hitrate` | Reads slow / cache miss | `hbase-metrics-cli blockcache-hitrate` |
| `wal-stats` | Writes slow | `hbase-metrics-cli wal-stats --since 30m` |
| `master-status` | Master / RIT issues | `hbase-metrics-cli master-status` |

## Common flags
`--cluster X` `--since 5m|1h` `--top N` `--format json|table|markdown` (default `json`) `--dry-run`

## Output contract
- **stdout** = JSON envelope `{scenario, cluster, range?, queries[].expr, columns, data[]}`
- **stderr** = structured errors `{error:{code, message, hint}}`
- **exit codes**: `0` success or NoData (warning on stderr) / `1` internal / `2` user error / `3` VM failure

## Diagnostic playbook — "HBase is slow"
Run in this order rather than going straight to one metric; later steps interpret earlier ones.

1. `cluster-overview` — overall severity & whether multiple RS are unhealthy
2. `rpc-latency` + `handler-queue` — service-side bottlenecks
3. `hotspot-detect` — single-RS hot spot driving the symptom
4. `gc-pressure` + `jvm-memory` — JVM dragging the RS
5. `compaction-status` + `blockcache-hitrate` — storage layer pressure
6. Drill in via `queries[].expr` (rerun with adjusted PromQL through `hbase-metrics-cli query '...'`)

## Escape hatch
For any case the 12 scenarios don't cover:

```bash
hbase-metrics-cli query 'sum by (instance) (rate(hadoop_hbase_totalrequestcount{cluster="mrs-hbase-oline"}[5m]))'
```

## Common errors
| Code | Action |
|---|---|
| `CONFIG_MISSING` | run `hbase-metrics-cli config init` |
| `VM_UNREACHABLE` | check VPN / DNS to `vm_url`, raise `--timeout` |
| `VM_HTTP_4XX` 401/403 | set `HBASE_VM_USER` / `HBASE_VM_PASS` |
| `NO_DATA` | confirm `--cluster` matches an active label, widen `--since` |
```

- [ ] **Step 2: Commit**

```bash
git add .claude/skills/hbase-metrics/SKILL.md
git commit -m "docs: add Claude Code skill for hbase-metrics"
```

---

## Task 20: Final integration — full lint pass and acceptance

**Files:** none new — verification + cleanup only.

- [ ] **Step 1: Run the full CI gate locally**

```bash
make tidy
git diff --exit-code go.mod go.sum   # must be empty
make lint
make unit-test
make e2e-dry
make release-snapshot
```

Expected: all green; `dist/` contains 4 binaries + `checksums.txt`.

- [ ] **Step 2: Live VM smoke (optional, requires network)**

```bash
./hbase-metrics-cli config init    # provide https://vm.rupiahcepatweb.com/ and mrs-hbase-oline
./hbase-metrics-cli cluster-overview --format table
./hbase-metrics-cli rpc-latency --top 5 --since 10m --format markdown
```

Expected: non-empty rows. If `NO_DATA`, the metric names in the YAML may differ from the actual exporter output — adjust per `Render` golden tests, not Go code.

- [ ] **Step 3: Acceptance check against spec §14**

Verify each acceptance criterion:

1. `cluster-overview` returns non-empty JSON envelope from a reachable VM. ✅ via Step 2.
2. All 12 scenarios pass dry-run e2e in CI. ✅ via `make e2e-dry`.
3. Golden PromQL files exist for all 12 scenarios. ✅ list `tests/golden/*.golden.promql` — must be 12.
4. README.md / README.zh.md document install + agent quick start; SKILL.md lists all 12 scenarios. ✅ visual.
5. CI gates green. ✅ Step 1.
6. `goreleaser release --snapshot` produces 4 platform binaries locally. ✅ Step 1.

```bash
ls tests/golden/*.golden.promql | wc -l   # expect 12
```

- [ ] **Step 4: Final commit (if any cleanup)**

```bash
git add -u
git diff --cached --quiet || git commit -m "chore: final integration polish"
```

- [ ] **Step 5: Tag v0.1.0 release candidate**

```bash
git tag -a v0.1.0 -m "v0.1.0 — initial release"
echo "Run: goreleaser release --clean   (when ready to publish)"
```

---

## Self-Review

**Spec coverage:**

| Spec section | Implementing task |
|---|---|
| §3 metrics pipeline & label conventions | Task 10–14 (YAML scenarios use `cluster`, `role`, `sub`, `instance`) |
| §4.1 12 scenarios | Tasks 10, 11, 12, 13, 14 |
| §4.2 utility commands (`config init/show`, `query`, `version`) | Tasks 7, 9 |
| §4.3 global flags | Task 7 |
| §4.4 common scenario flags (`--role`, `--instance`, `--since`, `--step`, `--top`) | Task 8 (`buildCmd`) + per-YAML `flags:` |
| §5 architecture / repo layout | Tasks 1, 2, 3, 4, 5, 6, 7, 8, 9 |
| §5.3 scenario YAML format | Task 6 schema, Tasks 10–14 instances |
| §6 data flow + JSON envelope | Tasks 4, 5, 8 (`Run` + `Render`) |
| §6.2 config resolution order | Task 3 (`Load` / `ApplyEnv` / `ApplyFlags`) |
| §7 error codes & hints | Task 2; Task 8 (`NO_DATA` warning), Task 5 (HTTP error mapping) |
| §8 testing layers | Task 2–6 unit; Tasks 10–14 golden; Task 15 e2e dry-run |
| §9 CI gates | Task 1 (`Makefile`); Task 20 verification |
| §10 SKILL.md | Task 19 |
| §11 docs (EN/ZH) | Tasks 17, 18 |
| §12 distribution (Makefile, GoReleaser) | Tasks 1, 16 |
| §14 acceptance criteria | Task 20 |

No spec gaps detected.

**Placeholder scan:** Re-read of every task — no `TBD`, no "implement later", no "similar to Task N", no "appropriate error handling". Every code step ships full code; every command step gives the exact invocation and expected output.

**Type consistency:**
- `Envelope` / `Row` / `Query` / `Range` defined in Task 4, used identically in Tasks 5, 8, 9.
- `Scenario` / `Rendered` / `Vars` defined in Task 6, used identically in Task 8.
- `CodedError` / codes / `WriteJSON` / `ExitCode` defined in Task 2, used identically in Tasks 3, 5, 7, 8, 9.
- `config.Config` / `FlagOverrides` / `ApplyEnv` / `ApplyFlags` / `Source*` defined in Task 3, used identically in Task 7.
- `vmclient.New` / `Options` / `Query` / `QueryRange` / `Result` / `Sample` defined in Task 5, used identically in Task 8 and Task 9.

No mismatches.
