// Package aggregate turns VictoriaMetrics range datapoints into fixed Summary
// scalars (min/max/avg/p50/p95/p99/last). It knows nothing about scenarios,
// HTTP, or output formatting — boundary contract per CLAUDE.md.
package aggregate

import (
	"math"
	"sort"
	"strconv"
)

type Summary struct {
	Count    int     `json:"count"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Avg      float64 `json:"avg"`
	P50      float64 `json:"p50"`
	P95      float64 `json:"p95"`
	P99      float64 `json:"p99"`
	Last     float64 `json:"last"`
	NaNRatio float64 `json:"nan_ratio,omitempty"`
}

// Summarize computes order statistics over a VictoriaMetrics datapoint slice.
// `values[i]` is `[unix_seconds, "value-string"]`. NaN / +Inf / -Inf are
// excluded from numeric stats but counted in NaNRatio. Returns Summary{Count:0}
// on empty input.
func Summarize(values [][2]any) Summary {
	if len(values) == 0 {
		return Summary{}
	}
	nums := make([]float64, 0, len(values))
	var lastValid float64
	var sawValid bool
	invalid := 0
	for _, v := range values {
		s, ok := v[1].(string)
		if !ok {
			invalid++
			continue
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			invalid++
			continue
		}
		nums = append(nums, f)
		lastValid = f
		sawValid = true
	}
	out := Summary{
		Count:    len(values),
		NaNRatio: float64(invalid) / float64(len(values)),
	}
	if !sawValid {
		return out
	}
	sorted := make([]float64, len(nums))
	copy(sorted, nums)
	sort.Float64s(sorted)
	out.Min = sorted[0]
	out.Max = sorted[len(sorted)-1]
	var sum float64
	for _, n := range nums {
		sum += n
	}
	out.Avg = sum / float64(len(nums))
	out.P50 = quantile(sorted, 0.50)
	out.P95 = quantile(sorted, 0.95)
	out.P99 = quantile(sorted, 0.99)
	out.Last = lastValid
	return out
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(q*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
