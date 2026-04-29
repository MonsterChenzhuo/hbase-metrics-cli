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

func TestRetry_RecoversAfter503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits < 3 {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second, MaxRetries: 3, RetryBaseDelay: 1 * time.Millisecond})
	_, err := c.Query(context.Background(), "up", time.Now())
	require.NoError(t, err)
	require.Equal(t, 3, hits, "should have retried twice after 2x 503 then succeeded")
}

func TestRetry_NoRetryOn401(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second, MaxRetries: 3, RetryBaseDelay: 1 * time.Millisecond})
	_, err := c.Query(context.Background(), "up", time.Now())
	require.Error(t, err)
	require.Equal(t, 1, hits, "401 is permanent and must not be retried")
}

func TestRetry_RetriesOn429(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second, MaxRetries: 2, RetryBaseDelay: 1 * time.Millisecond})
	_, err := c.Query(context.Background(), "up", time.Now())
	require.NoError(t, err)
	require.Equal(t, 2, hits)
}

func TestRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second, MaxRetries: 2, RetryBaseDelay: 1 * time.Millisecond})
	_, err := c.Query(context.Background(), "up", time.Now())
	require.Error(t, err)
	require.Equal(t, 3, hits, "should have made 1 initial + 2 retries = 3 attempts")
}

func TestSeries_ReturnsLabelSets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/series", r.URL.Path)
		require.Equal(t, `up{cluster="c1"}`, r.URL.Query().Get("match[]"))
		_, _ = w.Write([]byte(`{"status":"success","data":[{"__name__":"up","cluster":"c1","instance":"a"},{"__name__":"up","cluster":"c1","instance":"b"}]}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	out, err := c.Series(context.Background(), `up{cluster="c1"}`, 0)
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Equal(t, "a", out[0]["instance"])
}

// guard for unused import on Go versions
var _ = json.RawMessage{}
