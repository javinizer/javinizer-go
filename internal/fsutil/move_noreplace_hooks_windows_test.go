//go:build windows

package fsutil

import "testing"

// The Windows publish leg drives MoveFileExW without an injectable seam in
// this package; these helpers no-op and the caller skips.
func hookNoReplacePlantSeams(_ *testing.T, _ func()) bool                     { return false }
func forceNoReplaceEXDEV(_ *testing.T, _ int) bool                            { return false }
func swapFileAfterNPublishCalls(_ *testing.T, _ int, _ string, _ []byte) bool { return false }

func hookNoReplacePlantSeamsLater(_ *testing.T, _ int, _ func()) bool { return false }

func hookNoReplacePlantSeamsFirstCall(_ *testing.T, _ func()) bool { return false }
