package stepauto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolve_Boundaries(t *testing.T) {
	cases := []struct {
		name  string
		since time.Duration
		want  time.Duration
	}{
		{"5m", 5 * time.Minute, 30 * time.Second},
		{"30m exact", 30 * time.Minute, 30 * time.Second},
		{"31m", 31 * time.Minute, 1 * time.Minute},
		{"2h exact", 2 * time.Hour, 1 * time.Minute},
		{"3h", 3 * time.Hour, 2 * time.Minute},
		{"12h exact", 12 * time.Hour, 2 * time.Minute},
		{"24h exact", 24 * time.Hour, 5 * time.Minute},
		{"7d", 7 * 24 * time.Hour, 10 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, Resolve(c.since))
		})
	}
}

func TestResolve_Zero(t *testing.T) {
	require.Equal(t, 30*time.Second, Resolve(0))
	require.Equal(t, 30*time.Second, Resolve(-time.Hour))
}
