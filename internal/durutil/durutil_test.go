package durutil

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		// stdlib-compatible forms pass through unchanged
		{"30m", 30 * time.Minute, false},
		{"24h", 24 * time.Hour, false},
		{"1h30m", 90 * time.Minute, false},
		{"500ms", 500 * time.Millisecond, false},
		{"90s", 90 * time.Second, false},
		// day / week extensions
		{"1d", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"1w", 7 * 24 * time.Hour, false},
		// mixed forms
		{"1d12h", 36 * time.Hour, false},
		{"1w3d", 10 * 24 * time.Hour, false},
		{"1w3d12h", 10*24*time.Hour + 12*time.Hour, false},
		// signs
		{"-7d", -7 * 24 * time.Hour, false},
		{"+1d", 24 * time.Hour, false},
		// fractional
		{"0.5d", 12 * time.Hour, false},
		// errors
		{"", 0, true},
		{"7x", 0, true},
		{"d", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := Parse(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) expected error, got %v", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("Parse(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
