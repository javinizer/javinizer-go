//go:build !windows

package fsutil

func replacementBusyCanMarkReleased(error) bool { return false }
