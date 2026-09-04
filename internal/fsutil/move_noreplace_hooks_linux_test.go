//go:build linux

package fsutil

import (
	"os"
	"syscall"
	"testing"
)

// hookNoReplacePlantSeams installs a plant-then-delegate around every publish
// seam (renameat2 kernel leg and the hard-link fallback).
func hookNoReplacePlantSeams(t *testing.T, plant func()) bool {
	t.Helper()
	prevK := renameNoReplaceKernel
	prevL := publishNoReplaceLink
	renameNoReplaceKernel = func(s, d string) error { plant(); return prevK(s, d) }
	publishNoReplaceLink = func(s, d string) error { plant(); return prevL(s, d) }
	t.Cleanup(func() {
		renameNoReplaceKernel = prevK
		publishNoReplaceLink = prevL
	})
	return true
}

// forceNoReplaceEXDEV makes both publish seams fail with a wrapped EXDEV
// (simulating a cross-device source) for N calls each, then restores them.
func forceNoReplaceEXDEV(t *testing.T, nCalls int) bool {
	t.Helper()
	remaining := nCalls
	prevK := renameNoReplaceKernel
	prevL := publishNoReplaceLink
	delegate := func(prev func(string, string) error) func(string, string) error {
		return func(s, d string) error {
			if remaining > 0 {
				remaining--
				return syscall.EXDEV
			}
			return prev(s, d)
		}
	}
	renameNoReplaceKernel = delegate(prevK)
	publishNoReplaceLink = delegate(prevL)
	t.Cleanup(func() {
		renameNoReplaceKernel = prevK
		publishNoReplaceLink = prevL
	})
	return true
}

// swapFileAfterNPublishCalls replaces the file at path with new content after
// exactly n publish-seam calls (used to simulate a post-publish source swap).
func swapFileAfterNPublishCalls(t *testing.T, n int, path string, content []byte) bool {
	t.Helper()
	calls := 0
	prevK := renameNoReplaceKernel
	prevL := publishNoReplaceLink
	wrap := func(prev func(string, string) error) func(string, string) error {
		return func(s, d string) error {
			calls++
			if calls == n {
				if err := os.WriteFile(path, content, 0o644); err != nil {
					return err
				}
			}
			return prev(s, d)
		}
	}
	renameNoReplaceKernel = wrap(prevK)
	publishNoReplaceLink = wrap(prevL)
	t.Cleanup(func() {
		renameNoReplaceKernel = prevK
		publishNoReplaceLink = prevL
	})
	return true
}

// hookNoReplacePlantSeamsLater invokes plant() immediately BEFORE the n-th
// total publish-seam call (counting across both seams).
func hookNoReplacePlantSeamsLater(t *testing.T, n int, plant func()) bool {
	t.Helper()
	calls := 0
	prevK := renameNoReplaceKernel
	prevL := publishNoReplaceLink
	wrap := func(prev func(string, string) error) func(string, string) error {
		return func(s, d string) error {
			calls++
			if calls == n {
				plant()
			}
			return prev(s, d)
		}
	}
	renameNoReplaceKernel = wrap(prevK)
	publishNoReplaceLink = wrap(prevL)
	t.Cleanup(func() {
		renameNoReplaceKernel = prevK
		publishNoReplaceLink = prevL
	})
	return true
}

// hookNoReplacePlantSeamsFirstCall runs action() during the FIRST publish-seam
// call (before it binds anything), then restores.
func hookNoReplacePlantSeamsFirstCall(t *testing.T, action func()) bool {
	t.Helper()
	calls := 0
	prevK := renameNoReplaceKernel
	prevL := publishNoReplaceLink
	wrap := func(prev func(string, string) error) func(string, string) error {
		return func(s, d string) error {
			if calls == 0 {
				calls++
				action()
			}
			return prev(s, d)
		}
	}
	renameNoReplaceKernel = wrap(prevK)
	publishNoReplaceLink = wrap(prevL)
	t.Cleanup(func() {
		renameNoReplaceKernel = prevK
		publishNoReplaceLink = prevL
	})
	return true
}
