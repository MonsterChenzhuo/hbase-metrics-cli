// Package stepauto resolves a sensible PromQL `step` from a `since` window.
package stepauto

import "time"

// Resolve returns the step matching the time window. Caller-supplied step
// (e.g. --step 30s) takes priority over this; this function is only consulted
// when --step is "auto" or unset.
func Resolve(since time.Duration) time.Duration {
	switch {
	case since <= 0:
		return 30 * time.Second
	case since <= 30*time.Minute:
		return 30 * time.Second
	case since <= 2*time.Hour:
		return 1 * time.Minute
	case since <= 12*time.Hour:
		return 2 * time.Minute
	case since <= 24*time.Hour:
		return 5 * time.Minute
	default:
		return 10 * time.Minute
	}
}
