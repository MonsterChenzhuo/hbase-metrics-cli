package cmd

import "testing"

func TestMetricSelectorScopesHBaseMetricsToCluster(t *testing.T) {
	got := metricSelector("hadoop_hbase_", "mrs-hbase-oline")
	want := `{__name__=~"hadoop_hbase_.*",cluster="mrs-hbase-oline"}`
	if got != want {
		t.Fatalf("metricSelector() = %q, want %q", got, want)
	}
}

func TestMetricSelectorCanListAllMetrics(t *testing.T) {
	got := metricSelector("", "")
	if got != "" {
		t.Fatalf("metricSelector() = %q, want empty selector", got)
	}
}
