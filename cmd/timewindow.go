package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Why this file exists: every real incident investigation with this tool
// starts from an alarm that reports a *wall-clock time in a local timezone*
// (e.g. Beijing 11:57), while VictoriaMetrics and every scenario here speak
// UTC and `--since` can only look back from "now". That mismatch forced
// hand-computing Unix timestamps and piping raw curl for every drill-in, and
// caused at least one wrong-timezone dead end. `--end` + `--tz` let the query
// escape hatch pin an absolute window in the operator's own timezone.

// offsetPattern matches numeric UTC offsets like +08:00, +0800, -05:00, +8.
var offsetPattern = regexp.MustCompile(`^([+-])(\d{1,2})(?::?(\d{2}))?$`)

// parseLocation resolves a timezone spec into a *time.Location.
//
// Accepts:
//   - "" → UTC (unchanged default; callers that want UTC pass empty)
//   - an IANA name understood by the host tzdata ("Asia/Shanghai", "UTC")
//   - a fixed numeric offset ("+08:00", "+0800", "+8", "-05:00")
//
// A fixed offset is materialised as a named FixedZone ("UTC+08:00") so the
// rendered timestamps carry an unambiguous marker.
func parseLocation(tz string) (*time.Location, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return time.UTC, nil
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc, nil
	}
	if m := offsetPattern.FindStringSubmatch(tz); m != nil {
		sign := 1
		if m[1] == "-" {
			sign = -1
		}
		hours, _ := strconv.Atoi(m[2])
		mins := 0
		if m[3] != "" {
			mins, _ = strconv.Atoi(m[3])
		}
		if hours > 14 || mins >= 60 {
			return nil, fmt.Errorf("timezone offset %q out of range", tz)
		}
		secs := sign * (hours*3600 + mins*60)
		name := fmt.Sprintf("UTC%s%02d:%02d", m[1], hours, mins)
		return time.FixedZone(name, secs), nil
	}
	return nil, fmt.Errorf("unrecognized timezone %q: use an IANA name (Asia/Shanghai) or an offset (+08:00)", tz)
}

// endLayouts are the zone-less wall-clock forms accepted for --end. They are
// interpreted in the --tz location. RFC3339 (which carries its own zone) is
// tried first, before these.
var endLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
}

// parseEndTime resolves the --end value into an absolute instant.
//
// Accepts, in order:
//   - all-digit string → Unix seconds (UTC, zone-independent)
//   - RFC3339 with explicit zone ("2026-07-01T03:57:00Z", "...+08:00")
//   - a zone-less wall-clock form (see endLayouts), interpreted in loc
//
// loc must not be nil; pass time.UTC for the default.
func parseEndTime(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty --end")
	}
	if isAllDigits(s) {
		secs, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid unix timestamp %q: %v", s, err)
		}
		return time.Unix(secs, 0).UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range endLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized --end %q: use unix seconds, RFC3339, or \"2006-01-02 15:04:05\"", s)
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
