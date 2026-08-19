package fsutil

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/afero"
)

// ErrStagedIdentityMismatch classifies a publish-time proof failure
// (wave-29, codex P1, PR#215): the O_EXCL-created staged NAME no longer
// addresses the inode this process staged — a directory writer renamed the
// staging name away and planted a foreign object (typically a symlink) on it
// inside the stage→publish window. Callers MUST NOT publish by path after
// this error, MUST NOT remove the staged name (it is foreign-owned now), and
// classify the failed publish as name-unproven (history's rearm-refused
// pending kind): the staged inode itself may now live under a name this
// process never chose.
var ErrStagedIdentityMismatch = errors.New("staged name no longer names the exclusively staged inode")

// VerifyStagedIdentity proves — at publish time, immediately before a
// path-based publish — that the staged PATH still names the inode the
// caller's OPEN staging HANDLE addresses. An O_EXCL creation makes the
// staged inode provably ours at create time, but a directory writer can
// rename the name away and plant a symlink on it inside the copy/metadata
// window; publishing by the name alone would then move the plant (or hit the
// link target) instead of the staged bytes. The proof compares fh.Stat()
// (fstat — bound to the inode) with an Lstat of the staged name (bound to
// the directory entry WITHOUT following a planted link) through os.SameFile.
//
// The proof runs only for the REAL OsFs (osStagingHandle): virtual
// filesystems — afero MemMapFs included — have no rename-away/symlink threat
// model, and wrapper filesystems must observe path operations, never handle
// syscalls. On a name lookup failure the publish is refused too: an
// indeterminate staged name must never be treated as ours.
func VerifyStagedIdentity(fs afero.Fs, staged string, fh afero.File) error {
	if _, ok := osStagingHandle(fs, fh); !ok {
		return nil
	}
	handleInfo, err := fh.Stat()
	if err != nil {
		return fmt.Errorf("staged identity proof for %s: %w", staged, err)
	}
	var nameInfo os.FileInfo
	var lerr error
	if ls, ok := fs.(afero.Lstater); ok {
		nameInfo, _, lerr = ls.LstatIfPossible(staged)
	} else {
		nameInfo, lerr = fs.Stat(staged)
	}
	if lerr != nil {
		return fmt.Errorf("staged identity proof for %s: %w", staged, lerr)
	}
	if !os.SameFile(handleInfo, nameInfo) {
		return fmt.Errorf("%w: %s", ErrStagedIdentityMismatch, staged)
	}
	return nil
}
