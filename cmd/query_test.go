package cmd

import (
	"reflect"
	"testing"
	"time"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

func TestQueryColumns(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]struct{}
		want   []string
	}{
		{
			name:   "no extra labels",
			labels: map[string]struct{}{},
			want:   []string{"instance", "value"},
		},
		{
			name:   "single label keeps cluster column",
			labels: map[string]struct{}{"cluster": {}},
			want:   []string{"instance", "value", "cluster"},
		},
		{
			name:   "multiple labels sorted alphabetically",
			labels: map[string]struct{}{"role": {}, "cluster": {}, "service": {}},
			want:   []string{"instance", "value", "cluster", "role", "service"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := queryColumns(tt.labels)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("queryColumns(%v) = %v, want %v", tt.labels, got, tt.want)
			}
		})
	}
}

func TestQueryHasClusterFilter(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want bool
	}{
		{"plain equal", `up{cluster="c1"}`, true},
		{"regex match", `up{cluster=~"c.*"}`, true},
		{"not equal", `up{cluster!="c1"}`, true},
		{"negative regex", `up{cluster!~"c.*"}`, true},
		{"missing", `up{instance="a"}`, false},
		{"no labels", `up`, false},
		{"comment-like substring (not a real filter)", `up{role="cluster-admin"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := queryHasClusterFilter(tt.expr)
			if got != tt.want {
				t.Errorf("queryHasClusterFilter(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestQuerySampleTimestamp(t *testing.T) {
	tests := []struct {
		name   string
		in     []any
		wantTS int64
		wantOK bool
	}{
		{"float64", []any{float64(1700000000), "1.5"}, 1700000000, true},
		{"int64", []any{int64(1700000001), "2"}, 1700000001, true},
		{"empty", []any{}, 0, false},
		{"non-numeric ts", []any{"nope", "1"}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, ok := querySampleTimestamp(tt.in)
			if ts != tt.wantTS || ok != tt.wantOK {
				t.Errorf("querySampleTimestamp(%v) = (%d,%v), want (%d,%v)", tt.in, ts, ok, tt.wantTS, tt.wantOK)
			}
		})
	}
}

func TestQuerySampleValue(t *testing.T) {
	if got := querySampleValue([]any{float64(1700000000), "3.14"}); got != 3.14 {
		t.Errorf("querySampleValue numeric = %v, want 3.14", got)
	}
	if got := querySampleValue([]any{float64(1700000000)}); got != nil {
		t.Errorf("querySampleValue short = %v, want nil", got)
	}
	if got := querySampleValue([]any{float64(1700000000), "NaN-ish"}); got != "NaN-ish" {
		t.Errorf("querySampleValue non-parsable = %v, want passthrough string", got)
	}
}

// TestQueryRawRows locks the raw range flattening: one row per (instance,
// timestamp), sorted by instance then time, with numeric values parsed. This
// is the shape the escape hatch now emits so agents can find a peak minute.
func TestQueryRawRows(t *testing.T) {
	res := &vmclient.Result{
		Result: []vmclient.Sample{
			{
				Metric: map[string]string{"instance": "10.0.0.2:19110"},
				Values: [][]any{{float64(1700000060), "5"}},
			},
			{
				Metric: map[string]string{"instance": "10.0.0.1:19110"},
				Values: [][]any{
					{float64(1700000000), "1"},
					{float64(1700000030), "9"},
				},
			},
		},
	}
	rows := queryRawRows(res, time.UTC)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// Sorted: instance .1 (two points, ascending ts) before .2.
	if rows[0]["instance"] != "10.0.0.1:19110" || rows[0]["timestamp"].(int64) != 1700000000 {
		t.Errorf("row[0] = %+v, want first .1 point", rows[0])
	}
	if rows[1]["timestamp"].(int64) != 1700000030 || rows[1]["value"] != 9.0 {
		t.Errorf("row[1] = %+v, want second .1 point value 9", rows[1])
	}
	if rows[2]["instance"] != "10.0.0.2:19110" {
		t.Errorf("row[2] = %+v, want .2 point", rows[2])
	}
	if rows[0]["time"] != "2023-11-14T22:13:20Z" {
		t.Errorf("row[0][time] = %v, want RFC3339 UTC", rows[0]["time"])
	}
}

// TestQueryInstantRows verifies instant projection keeps instance + value and
// captures extra labels for the column header.
func TestQueryInstantRows(t *testing.T) {
	res := &vmclient.Result{
		Result: []vmclient.Sample{
			{
				Metric: map[string]string{"instance": "10.0.0.1:19110", "cluster": "c1"},
				Value:  []any{float64(1700000000), "42"},
			},
		},
	}
	rows, labelSet := queryInstantRows(res)
	if len(rows) != 1 || rows[0]["value"] != "42" || rows[0]["cluster"] != "c1" {
		t.Errorf("rows = %+v, want value 42 + cluster c1", rows)
	}
	if _, ok := labelSet["cluster"]; !ok {
		t.Errorf("labelSet = %v, want cluster captured", labelSet)
	}
}
