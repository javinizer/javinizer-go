//go:build windows

package main

// applyUmask is a no-op on Windows.
func applyUmask(_ int) (oldMask int, applied bool) {
	return 0, false
}
