package cmd

import (
	"testing"
	"time"

	"github.com/opay-bigdata/hbase-metrics-cli/internal/vmclient"
)

func TestParseLocation(t *testing.T) {
	tests := []struct {
		name       string
		tz         string
		wantOffset int // seconds east of UTC, checked at a fixed instant
		wantErr    bool
	}{
		{name: "empty is UTC", tz: "", wantOffset: 0},
		{name: "UTC name", tz: "UTC", wantOffset: 0},
		{name: "offset colon", tz: "+08:00", wantOffset: 8 * 3600},
		{name: "offset no colon", tz: "+0800", wantOffset: 8 * 3600},
		{name: "offset short", tz: "+8", wantOffset: 8 * 3600},
		{name: "negative offset", tz: "-05:00", wantOffset: -5 * 3600},
		{name: "offset with minutes", tz: "+05:30", wantOffset: 5*3600 + 30*60},
		{name: "out of range", tz: "+15:00", wantErr: true},
		{name: "garbage", tz: "not-a-zone", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc, err := parseLocation(tt.tz)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseLocation(%q) expected error, got nil", tt.tz)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLocation(%q) unexpected error: %v", tt.tz, err)
			}
			// Check the offset at a fixed winter instant to avoid DST ambiguity
			// (all test zones here are fixed-offset or UTC anyway).
			ref := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
			_, off := ref.In(loc).Zone()
			if off != tt.wantOffset {
				t.Errorf("parseLocation(%q) offset = %d, want %d", tt.tz, off, tt.wantOffset)
			}
		})
	}
}

// TestParseLocation_IANA is separate because IANA lookup depends on host tzdata;
// skip cleanly when Asia/Shanghai is unavailable rather than failing the suite.
func TestParseLocation_IANA(t *testing.T) {
	loc, err := parseLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("Asia/Shanghai unavailable on this host: %v", err)
	}
	ref := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	_, off := ref.In(loc).Zone()
	if off != 8*3600 {
		t.Errorf("Asia/Shanghai offset = %d, want %d", off, 8*3600)
	}
}

func TestParseEndTime(t *testing.T) {
	shanghai := time.FixedZone("UTC+08:00", 8*3600)

	tests := []struct {
		name    string
		in      string
		loc     *time.Location
		wantUTC string // expected RFC3339 in UTC
		wantErr bool
	}{
		{
			name:    "unix seconds ignores tz",
			in:      "1782877020", // 2026-07-01T03:37:00Z
			loc:     shanghai,
			wantUTC: "2026-07-01T03:37:00Z",
		},
		{
			name:    "rfc3339 with zone",
			in:      "2026-07-01T03:57:00Z",
			loc:     shanghai,
			wantUTC: "2026-07-01T03:57:00Z",
		},
		{
			name:    "rfc3339 with offset",
			in:      "2026-07-01T11:57:00+08:00",
			loc:     time.UTC,
			wantUTC: "2026-07-01T03:57:00Z",
		},
		{
			name:    "wall clock parsed in tz",
			in:      "2026-07-01 11:57:00",
			loc:     shanghai,
			wantUTC: "2026-07-01T03:57:00Z",
		},
		{
			name:    "wall clock no seconds",
			in:      "2026-07-01 11:57",
			loc:     shanghai,
			wantUTC: "2026-07-01T03:57:00Z",
		},
		{
			name:    "wall clock in UTC",
			in:      "2026-07-01 03:57:00",
			loc:     time.UTC,
			wantUTC: "2026-07-01T03:57:00Z",
		},
		{name: "empty", in: "", loc: time.UTC, wantErr: true},
		{name: "garbage", in: "yesterday", loc: time.UTC, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEndTime(tt.in, tt.loc)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseEndTime(%q) expected error, got %v", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEndTime(%q) unexpected error: %v", tt.in, err)
			}
			if gotUTC := got.UTC().Format(time.RFC3339); gotUTC != tt.wantUTC {
				t.Errorf("parseEndTime(%q) = %s, want %s", tt.in, gotUTC, tt.wantUTC)
			}
		})
	}
}

// TestQueryRawRows_TimeLocal verifies the opt-in local-time column: UTC loc
// omits it (unchanged contract), a non-UTC loc adds time_local alongside the
// UTC time.
func TestQueryRawRows_TimeLocal(t *testing.T) {
	res := &vmclient.Result{
		Result: []vmclient.Sample{
			{
				Metric: map[string]string{"instance": "10.0.0.1:19110"},
				Values: [][]any{{float64(1782877020), "42"}}, // 2026-07-01T03:37:00Z
			},
		},
	}

	utcRows := queryRawRows(res, time.UTC)
	if _, ok := utcRows[0]["time_local"]; ok {
		t.Errorf("UTC loc should not emit time_local, got %+v", utcRows[0])
	}

	shanghai := time.FixedZone("UTC+08:00", 8*3600)
	localRows := queryRawRows(res, shanghai)
	got, ok := localRows[0]["time_local"].(string)
	if !ok {
		t.Fatalf("non-UTC loc should emit time_local, got %+v", localRows[0])
	}
	if want := "2026-07-01T11:37:00+08:00"; got != want {
		t.Errorf("time_local = %s, want %s", got, want)
	}
	// The UTC time column must stay unchanged regardless of loc.
	if localRows[0]["time"] != "2026-07-01T03:37:00Z" {
		t.Errorf("time column drifted: %v", localRows[0]["time"])
	}
}
