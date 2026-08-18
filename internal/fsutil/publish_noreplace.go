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

// ErrPublishNoReplaceUnsupported means the destination filesystem cannot
// express an ATOMIC no-replace publish at all — the kernel no-replace
// primitive (renameat2 RENAME_NOREPLACE) is unimplemented/rejected AND
// hard links are unsupported (FAT/exFAT-class media volumes answer link(2)
// with EPERM/ENOSYS/EOPNOTSUPP/ENOTSUP). The pre-wave-17 chain degraded to
// classify-then-rename there, which is NOT atomic: a foreign file created at
// the destination inside the window was silently overwritten, weakening every
// caller's collision guarantee to nothing on exactly the volumes that need it
// (POSTER-WRITE-HARDENING wave-17, codex P2). Callers MUST treat
// errors.Is(err, ErrPublishNoReplaceUnsupported) as a REFUSAL — the same
// conservative fail/kept-with-warn leg they apply to ErrPublishCollision —
// never as license to fall back to replacing semantics.
var ErrPublishNoReplaceUnsupported = errors.New("filesystem cannot express an atomic no-replace publish")

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
