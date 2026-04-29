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

func TestEnvelope_ModeSerialized(t *testing.T) {
	env := Envelope{
		Scenario: "x",
		Cluster:  "c",
		Mode:     "summary",
		Queries:  []Query{{Label: "l", Expr: "e"}},
		Columns:  []string{"a"},
		Data:     []Row{{"a": 1}},
	}
	var buf bytes.Buffer
	require.NoError(t, Render("json", env, &buf))
	require.Contains(t, buf.String(), `"mode": "summary"`)
}
