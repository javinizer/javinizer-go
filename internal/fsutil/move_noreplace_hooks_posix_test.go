//go:build darwin || freebsd || openbsd || netbsd || dragonfly || solaris

package fsutil

import (
	"os"
	"syscall"
	"testing"
)

// On non-Linux POSIX, PublishNoReplace's only seam is the hard-link fallback,
// which serves both the initial same-volume publish and the staged publish.
func hookNoReplacePlantSeams(t *testing.T, plant func()) bool {
	t.Helper()
	prev := publishNoReplaceLink
	publishNoReplaceLink = func(s, d string) error { plant(); return prev(s, d) }
	t.Cleanup(func() { publishNoReplaceLink = prev })
	return true
}

func forceNoReplaceEXDEV(t *testing.T, nCalls int) bool {
	t.Helper()
	remaining := nCalls
	prev := publishNoReplaceLink
	publishNoReplaceLink = func(s, d string) (err error) {
		if remaining > 0 {
			remaining--
			return &os.LinkError{Op: "link", Old: s, New: d, Err: syscall.EXDEV}
		}
		return prev(s, d)
	}
	t.Cleanup(func() { publishNoReplaceLink = prev })
	return true
}

func swapFileAfterNPublishCalls(t *testing.T, n int, path string, content []byte) bool {
	t.Helper()
	calls := 0
	prev := publishNoReplaceLink
	publishNoReplaceLink = func(s, d string) (err error) {
		calls++
		if calls == n {
			if werr := os.WriteFile(path, content, 0o644); werr != nil {
				return werr
			}
		}
		return prev(s, d)
	}
	t.Cleanup(func() { publishNoReplaceLink = prev })
	return true
}

// hookNoReplacePlantSeamsLater invokes plant() immediately BEFORE the n-th
// publish-link seam call.
func hookNoReplacePlantSeamsLater(t *testing.T, n int, plant func()) bool {
	t.Helper()
	calls := 0
	prev := publishNoReplaceLink
	publishNoReplaceLink = func(s, d string) (err error) {
		calls++
		if calls == n {
			plant()
		}
		return prev(s, d)
	}
	t.Cleanup(func() { publishNoReplaceLink = prev })
	return true
}

// hookNoReplacePlantSeamsFirstCall runs action() during the FIRST
// publish-link seam call, then restores.
func hookNoReplacePlantSeamsFirstCall(t *testing.T, action func()) bool {
	t.Helper()
	calls := 0
	prev := publishNoReplaceLink
	publishNoReplaceLink = func(s, d string) (err error) {
		if calls == 0 {
			calls++
			action()
		}
		return prev(s, d)
	}
	t.Cleanup(func() { publishNoReplaceLink = prev })
	return true
}
