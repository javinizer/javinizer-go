package downloader

import (
	"errors"
	"fmt"
	"os"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
)

// ErrRestoreSourceRefused identifies a rollback source that is not safe to
// open as a regular, non-symlink file.
var ErrRestoreSourceRefused = errors.New("restore source refused")

// RestoreSourceRefusedError describes a rollback source refusal without
// losing the errors.Is classification callers use for safe retry posture.
type RestoreSourceRefusedError struct {
	Backup string
	Reason string
}

func (e *RestoreSourceRefusedError) Error() string {
	return fmt.Sprintf("restore source %s refused: %s", e.Backup, e.Reason)
}

func (e *RestoreSourceRefusedError) Unwrap() error { return ErrRestoreSourceRefused }

func refuseRestoreSource(backup, reason string) error {
	logging.Warnf("downloader rollback restore refused for backup %s: %s; backup and journal retained", backup, reason)
	return &RestoreSourceRefusedError{Backup: backup, Reason: reason}
}

// lstatRestoreSource describes the final backup path component without
// following a symlink when the injected filesystem supports afero.Lstater.
// MemMapFs has no symlink model and safely falls back to Stat.
func lstatRestoreSource(fs afero.Fs, backup string) (os.FileInfo, error) {
	if ls, ok := fs.(afero.Lstater); ok {
		info, _, err := ls.LstatIfPossible(backup)
		return info, err
	}
	return fs.Stat(backup)
}
