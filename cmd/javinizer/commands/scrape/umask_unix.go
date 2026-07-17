//go:build !windows

package scrape

import "syscall"

func applyUmask(mask int) {
	syscall.Umask(mask)
}
