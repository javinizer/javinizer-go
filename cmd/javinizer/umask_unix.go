//go:build !windows

package main

import "syscall"

// applyUmask applies process umask on platforms that support it.
func applyUmask(mask int) (oldMask int, applied bool) {
	return syscall.Umask(mask), true
}
