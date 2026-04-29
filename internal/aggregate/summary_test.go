package aggregate

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func datapoints(vals ...string) [][2]any {
	out := make([][2]any, len(vals))
	for i, v := range vals {
		out[i] = [2]any{float64(1_700_000_000 + i*30), v}
	}
	return out
}

func TestSummarize_Empty(t *testing.T) {
	s := Summarize(nil)
	require.Equal(t, 0, s.Count)
	require.Zero(t, s.Max)
	require.Zero(t, s.Avg)
}

func TestSummarize_SinglePoint(t *testing.T) {
	s := Summarize(datapoints("42"))
	require.Equal(t, 1, s.Count)
	require.Equal(t, 42.0, s.Min)
	require.Equal(t, 42.0, s.Max)
	require.Equal(t, 42.0, s.Avg)
	require.Equal(t, 42.0, s.P50)
	require.Equal(t, 42.0, s.P99)
	require.Equal(t, 42.0, s.Last)
}

func TestSummarize_OrderStatistics(t *testing.T) {
	vals := make([]string, 100)
	for i := 0; i < 100; i++ {
		vals[i] = fmtFloat(float64(i + 1))
	}
	s := Summarize(datapoints(vals...))
	require.Equal(t, 100, s.Count)
	require.Equal(t, 1.0, s.Min)
	require.Equal(t, 100.0, s.Max)
	require.InDelta(t, 50.5, s.Avg, 0.01)
	require.InDelta(t, 50.0, s.P50, 1.0)
	require.InDelta(t, 95.0, s.P95, 1.0)
	require.InDelta(t, 99.0, s.P99, 1.0)
	require.Equal(t, 100.0, s.Last)
}

func TestSummarize_ResetSpike(t *testing.T) {
	vals := make([]string, 100)
	for i := range vals {
		vals[i] = "200"
	}
	vals[42] = "13129"
	s := Summarize(datapoints(vals...))
	require.Equal(t, 13129.0, s.Max)
	require.InDelta(t, 200.0, s.P99, 1.0, "p99 must not be polluted by the reset spike")
	require.InDelta(t, 200.0, s.P50, 1.0)
}

func TestSummarize_NaNAndInfExcluded(t *testing.T) {
	s := Summarize(datapoints("10", "NaN", "+Inf", "20", "30"))
	require.Equal(t, 5, s.Count)
	require.Equal(t, 10.0, s.Min)
	require.Equal(t, 30.0, s.Max)
	require.InDelta(t, 20.0, s.Avg, 0.01)
	require.InDelta(t, 0.4, s.NaNRatio, 0.01)
}

func TestSummarize_AllInvalid(t *testing.T) {
	s := Summarize(datapoints("NaN", "+Inf"))
	require.Equal(t, 2, s.Count)
	require.Equal(t, 1.0, s.NaNRatio)
	require.True(t, math.IsNaN(s.Avg) || s.Avg == 0)
}

func fmtFloat(f float64) string {
	return strconvFmt(f)
}
