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

	// MaxRetries is the number of retry attempts for transient failures
	// (network errors, HTTP 429, HTTP 5XX). Zero defaults to 2.
	MaxRetries int
	// RetryBaseDelay is the initial backoff delay; doubled each attempt.
	// Zero defaults to 200ms.
	RetryBaseDelay time.Duration
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
	if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	} else if opts.MaxRetries == 0 {
		opts.MaxRetries = 2
	}
	if opts.RetryBaseDelay <= 0 {
		opts.RetryBaseDelay = 200 * time.Millisecond
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

// Series mirrors a single entry returned by /api/v1/series — a label-set with
// no values. Used by SeriesMetadata().
type Series map[string]string

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

// Series queries /api/v1/series for the given selector and returns the
// label-sets of all matching series. matchExpr is a Prometheus selector like
// `hadoop_hbase_clusterrequests{cluster="x"}`.
func (c *Client) Series(ctx context.Context, matchExpr string, since time.Duration) ([]Series, error) {
	q := url.Values{}
	q.Set("match[]", matchExpr)
	if since > 0 {
		now := time.Now()
		q.Set("start", strconv.FormatInt(now.Add(-since).Unix(), 10))
		q.Set("end", strconv.FormatInt(now.Unix(), 10))
	}
	body, err := c.doRaw(ctx, "/api/v1/series", q)
	if err != nil {
		return nil, err
	}
	var ar apiResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, cerrors.Errorf(cerrors.CodeVMHTTP5XX, "decode series response: %v", err)
	}
	if ar.Status != "success" {
		return nil, cerrors.Errorf(cerrors.CodeVMHTTP4XX, "%s: %s", ar.ErrorType, ar.Error)
	}
	var out []Series
	if err := json.Unmarshal(ar.Data, &out); err != nil {
		return nil, cerrors.Errorf(cerrors.CodeVMHTTP5XX, "decode series data: %v", err)
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, path string, q url.Values) (*Result, error) {
	body, err := c.doRaw(ctx, path, q)
	if err != nil {
		return nil, err
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

// doRaw performs the HTTP GET with retry-on-transient-error and returns the
// raw response body if the status was 2xx. Error mapping into CodedError is
// the same as before; callers are responsible for unmarshaling the envelope.
func (c *Client) doRaw(ctx context.Context, path string, q url.Values) ([]byte, error) {
	base := strings.TrimRight(c.opts.BaseURL, "/")
	full := base + path + "?" + q.Encode()

	var lastErr error
	delay := c.opts.RetryBaseDelay
	attempts := c.opts.MaxRetries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, cerrors.Errorf(cerrors.CodeVMUnreachable, "%s: %v", base, ctx.Err())
			case <-time.After(delay):
			}
			delay *= 3
		}

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
			lastErr = cerrors.Errorf(cerrors.CodeVMUnreachable, "%s: %v", base, err)
			continue // network error → retry
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode >= 500 {
			lastErr = cerrors.Errorf(cerrors.CodeVMHTTP5XX, "%s returned %d: %s", base, resp.StatusCode, truncate(body, 200))
			continue // 5XX → retry
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = cerrors.Errorf(cerrors.CodeVMHTTP4XX, "%s returned 429: %s", base, truncate(body, 200))
			continue // 429 → retry
		}
		if resp.StatusCode >= 400 {
			// Permanent 4XX (auth/parse/not-found): do not retry.
			return nil, cerrors.Errorf(cerrors.CodeVMHTTP4XX, "%s returned %d: %s", base, resp.StatusCode, truncate(body, 200))
		}
		return body, nil
	}
	return nil, lastErr
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return fmt.Sprintf("%s...(truncated)", b[:n])
}
