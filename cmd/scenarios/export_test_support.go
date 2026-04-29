package scenarios

import (
	"github.com/opay-bigdata/hbase-metrics-cli/internal/output"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/promql"
	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

// BuildEnvelopeForGolden is a stable export used only by tests in
// other packages (e.g. tests/golden) to lock the envelope shape.
func BuildEnvelopeForGolden(s promql.Scenario, rendered []promql.Rendered, results []vmclient.Result, mode string) output.Envelope {
	return buildEnvelope(s, rendered, results, mode)
}
