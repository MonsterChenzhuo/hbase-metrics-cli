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
