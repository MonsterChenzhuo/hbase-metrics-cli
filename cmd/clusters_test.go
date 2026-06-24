package cmd

import "testing"

func TestClustersSelectorScopesToHBase(t *testing.T) {
	got := clustersSelector()
	want := `{__name__=~"hadoop_hbase_.*"}`
	if got != want {
		t.Fatalf("clustersSelector() = %q, want %q", got, want)
	}
}
