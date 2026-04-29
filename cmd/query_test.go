package cmd

import "testing"

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
