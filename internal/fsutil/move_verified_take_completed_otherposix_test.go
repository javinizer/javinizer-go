//go:build !linux && !windows

package fsutil

import "testing"

// Non-Linux POSIX targets expose no kernel no-replace rename, so the take hop
// publishes through publishNoReplaceFallback (the hard-link leg) by
// construction — the residue shape the strip scenario needs without any wedge.
func forceVerifiedTakeFallbackLeg(t *testing.T) {
	t.Helper()
}
