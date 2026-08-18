//go:build !windows

package fsutil

import (
	"os"
	"syscall"
)

var replacementFindProcess = os.FindProcess

func replacementProbePIDAliveAwarePlatform(pid int) replacementPIDLiveness {
	if replacementProcessAlive(pid) {
		return replacementPIDAlive
	}
	// POSIX keeps the existing conservative liveness contract: a failed
	// FindProcess/Signal(0) probe is treated as a dead owner.
	return replacementPIDDead
}

func replacementProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := replacementFindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}
