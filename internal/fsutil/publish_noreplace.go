package fsutil

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/afero"
)

// ErrPublishCollision classifies a no-replace publish whose destination was
// occupied by a foreign writer between the caller's existence classification
// and the publish itself (POSTER-WRITE-HARDENING wave-15, codex P2): a plain
// ReplaceFile/rename would destroy the racer's bytes with no backup and no
// ledger entry. Callers reclassify on errors.Is(err, ErrPublishCollision)
// instead of retrying the publish against the same name.
var ErrPublishCollision = errors.New("no-replace publish destination already occupied")

// publishCollision wraps the destination name in the collision class.
func publishCollision(dst string) error {
	return fmt.Errorf("%w: %s", ErrPublishCollision, dst)
}

// publishNoReplaceVirtual is PublishNoReplace's non-OsFs leg: OsFs routes to
// the platform's kernel no-replace primitive (publish_noreplace_unix.go /
// publish_noreplace_windows.go), while a virtual filesystem can only offer
// classify-then-rename. The Lstat classification refuses an already-occupied
// destination, and — because the classify→rename window cannot be closed
// portably on virtual filesystems — a rename that REFUSES an occupied
// destination (Windows MoveFileW semantics, test seam filesystems playing the
// kernel's role) maps its os.IsExist-class refusal into the same collision
// signal. Callers therefore observe exactly one race classification no matter
// which leg detected it.
func publishNoReplaceVirtual(fs afero.Fs, src, dst string) error {
	var err error
	if ls, ok := fs.(afero.Lstater); ok {
		_, _, err = ls.LstatIfPossible(dst)
	} else {
		_, err = fs.Stat(dst)
	}
	switch {
	case err == nil:
		// Any successful classification — symlink included, Lstat never
		// follows — means the destination is occupied.
		return publishCollision(dst)
	case !os.IsNotExist(err):
		// An indeterminate destination (permissions/IO) fails closed: the
		// publish must not gamble with possibly-present foreign bytes.
		return fmt.Errorf("classify no-replace publish destination %s: %w", dst, err)
	}
	if err := fs.Rename(src, dst); err != nil {
		if os.IsExist(err) {
			return publishCollision(dst)
		}
		return err
	}
	return nil
}
