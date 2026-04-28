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
