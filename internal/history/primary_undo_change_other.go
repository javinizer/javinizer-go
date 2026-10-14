//go:build !windows && !linux && !openbsd && !solaris && !dragonfly && !darwin && !freebsd && !netbsd

package history

import (
	"fmt"
	"github.com/spf13/afero"
)

func primaryUndoObserveChange(afero.File) (primaryUndoChangeWitness, error) {
	return primaryUndoChangeWitness{}, fmt.Errorf("primary Undo mutation witness is unsupported on this platform")
}
