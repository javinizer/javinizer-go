//go:build !windows && !linux

package fsutil

import "time"

// Non-Linux hosts do not have the /proc start-time seam; classification uses
// the age fallback in replacementBusyState.
func replacementProcessStartTimePlatform(int) *time.Time { return nil }
