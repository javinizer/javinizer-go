//go:build !windows && !linux

package fsutil

import "time"

// Non-Linux hosts do not have the /proc start-time seam; a positive liveness
// result remains busy and only an undecidable probe reaches the age fallback.
func replacementProcessStartTimePlatform(int) *time.Time { return nil }
