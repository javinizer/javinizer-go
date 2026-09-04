//go:build windows

package config

// Windows has no umask concept; the effective mask is always 0.
func currentProcessUmask() int { return 0 }
