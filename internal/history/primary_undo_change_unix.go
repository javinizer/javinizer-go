//go:build linux || openbsd || solaris || dragonfly

package history

import (
	"fmt"
	"github.com/spf13/afero"
	"syscall"
)

func primaryUndoObserveChange(file afero.File) (primaryUndoChangeWitness, error) {
	info, err := file.Stat()
	if err != nil {
		return primaryUndoChangeWitness{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return primaryUndoChangeWitness{}, fmt.Errorf("primary Undo requires a native mutation witness")
	}
	return primaryUndoChangeWitness{Seconds: int64(stat.Ctim.Sec), Nanoseconds: int64(stat.Ctim.Nsec)}, nil
}
