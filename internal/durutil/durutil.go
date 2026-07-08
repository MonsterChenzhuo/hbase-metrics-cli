// Package durutil parses human-friendly durations that extend Go's
// time.ParseDuration with day (d) and week (w) units.
//
// Why this exists: every scenario and the query escape hatch take a --since
// window, and operators (and the agent skill docs) naturally reach for "7d"
// or "2w" for multi-day health checks. Go's time.ParseDuration only knows up
// to "h", so "7d" hard-errored with `unknown unit "d"` and forced manual
// conversion to "168h" — a footgun hit on the very first 7-day query. Parse
// centralizes the extension so --since and --step behave identically.
package durutil

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	day  = 24 * time.Hour
	week = 7 * day
)

// Parse behaves like time.ParseDuration but additionally understands "d"
// (days) and "w" (weeks) units, and mixed forms like "1w3d12h". It accepts
// everything time.ParseDuration accepts (ns/us/ms/s/m/h) unchanged, so it is
// a drop-in replacement.
//
// Units may be combined and repeated in any order ("36h", "1d12h", "2w").
// A leading sign ("-7d") is honored. An empty string is an error, matching
// time.ParseDuration.
func Parse(s string) (time.Duration, error) {
	orig := s
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	if s == "" {
		return 0, fmt.Errorf("invalid duration %q", orig)
	}

	// Fast path: no day/week units → defer entirely to the stdlib so we match
	// its parsing and error messages exactly for the common case.
	if !strings.ContainsAny(s, "dwDW") {
		return time.ParseDuration(orig)
	}

	var total time.Duration
	// stdRemainder accumulates any sub-day units so we can hand them to the
	// stdlib parser in one shot (preserving its fractional handling).
	var stdRemainder strings.Builder

	i := 0
	for i < len(s) {
		// Read the numeric part (digits and an optional decimal point).
		start := i
		for i < len(s) && (unicode.IsDigit(rune(s[i])) || s[i] == '.') {
			i++
		}
		if start == i {
			return 0, fmt.Errorf("invalid duration %q", orig)
		}
		numStr := s[start:i]

		// Read the unit (letters).
		unitStart := i
		for i < len(s) && unicode.IsLetter(rune(s[i])) {
			i++
		}
		if unitStart == i {
			return 0, fmt.Errorf("missing unit in duration %q", orig)
		}
		unit := s[unitStart:i]

		switch unit {
		case "d":
			v, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q", orig)
			}
			total += time.Duration(v * float64(day))
		case "w":
			v, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q", orig)
			}
			total += time.Duration(v * float64(week))
		default:
			// A sub-day unit (ns/us/ms/s/m/h) — let the stdlib handle it.
			stdRemainder.WriteString(numStr)
			stdRemainder.WriteString(unit)
		}
	}

	if stdRemainder.Len() > 0 {
		d, err := time.ParseDuration(stdRemainder.String())
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", orig)
		}
		total += d
	}

	if neg {
		total = -total
	}
	return total, nil
}
