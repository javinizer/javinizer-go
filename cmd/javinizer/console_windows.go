//go:build windows

package main

import (
	"os"
	"syscall"
)

// AttachConsole is not exposed by the stdlib syscall package, so bind it
// manually from kernel32. GetStdHandle + the STD_* constants are stdlib.
var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
)

// attachParentProcess is the special handle value passed to AttachConsole to
// attach to the console of the process that launched us (e.g. cmd.exe). It is
// (DWORD)-1; the stdlib does not name it.
const attachParentProcess = ^uintptr(0)

// attachParentConsole is a var (not a func) so tests can assert the
// desktop-build branch in run() takes it; on Windows it does the real work.
var attachParentConsole = func() {
	r, _, _ := procAttachConsole.Call(attachParentProcess)
	if r == 0 {
		return
	}
	if h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != syscall.InvalidHandle {
		os.Stdout = os.NewFile(uintptr(h), "stdout")
	}
	if h, err := syscall.GetStdHandle(syscall.STD_ERROR_HANDLE); err == nil && h != 0 && h != syscall.InvalidHandle {
		os.Stderr = os.NewFile(uintptr(h), "stderr")
	}
}
