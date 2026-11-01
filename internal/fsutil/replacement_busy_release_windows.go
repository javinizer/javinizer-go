//go:build windows

package fsutil

import (
	"errors"

	"golang.org/x/sys/windows"
)

func replacementBusyCanMarkReleased(err error) bool {
	// Foreign, vanished or incompletely restored objects stay refused even
	// when a joined cleanup error also reports a sharing violation.
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) &&
		!errors.Is(err, ErrTakeAsideForeign) &&
		!errors.Is(err, ErrTakeAsideVanished) &&
		!errors.Is(err, ErrTakeAsideRestoreFailed)
}
