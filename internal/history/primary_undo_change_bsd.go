//go:build darwin || freebsd || netbsd

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
	seconds, nanoseconds := stat.Ctimespec.Unix()
	return primaryUndoChangeWitness{Seconds: seconds, Nanoseconds: nanoseconds}, nil
}
