//go:build !windows

package config

import "syscall"

// currentProcessUmask probes the process's inherited umask via set-and-restore.
// Safe at package init (nothing else runs concurrently yet).
func currentProcessUmask() int {
	m := syscall.Umask(0)
	syscall.Umask(m)
	return m
}
