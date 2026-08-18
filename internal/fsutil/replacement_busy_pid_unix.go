//go:build !windows

package fsutil

import (
	"errors"
	"os"
	"syscall"
)

var replacementFindProcess = os.FindProcess
var replacementSignalZero = func(p *os.Process) error {
	return p.Signal(syscall.Signal(0))
}

func replacementProbePIDAliveAwarePlatform(pid int) replacementPIDLiveness {
	return replacementProcessLiveness(pid)
}

// replacementProcessLiveness keeps POSIX probe outcomes distinct: ESRCH is a
// proof that the process is gone, while other inspection failures are
// undecidable and may use the classifier's age fallback.
func replacementProcessLiveness(pid int) replacementPIDLiveness {
	if pid <= 0 {
		return replacementPIDDead
	}
	process, err := replacementFindProcess(pid)
	if err != nil {
		return replacementPIDUnprobeable
	}
	err = replacementSignalZero(process)
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return replacementPIDAlive
	case errors.Is(err, syscall.ESRCH), errors.Is(err, os.ErrProcessDone):
		return replacementPIDDead
	default:
		return replacementPIDUnprobeable
	}
}
