//go:build windows

package main

import (
	"os"
	"syscall"
)

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procGetStdHandle  = kernel32.NewProc("GetStdHandle")
)

const (
	attachParentProcess = ^uintptr(0)
	stdOutputHandle     = ^uintptr(10)
	stdErrorHandle      = ^uintptr(11)
	invalidHandleValue  = ^uintptr(0)
)

func attachParentConsole() {
	r, _, _ := procAttachConsole.Call(attachParentProcess)
	if r == 0 {
		return
	}
	if out, _, _ := procGetStdHandle.Call(stdOutputHandle); out != 0 && out != invalidHandleValue {
		os.Stdout = os.NewFile(out, "stdout")
	}
	if err, _, _ := procGetStdHandle.Call(stdErrorHandle); err != 0 && err != invalidHandleValue {
		os.Stderr = os.NewFile(err, "stderr")
	}
}
