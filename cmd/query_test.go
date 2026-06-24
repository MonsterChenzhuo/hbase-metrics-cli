package cmd

import (
	"reflect"
	"testing"
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
