//go:build windows

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// publishStagedBoundOS is PublishStagedBound's Windows leg. MoveFileEx
// cannot rename a file with an open Go handle (Go opens without
// FILE_SHARE_DELETE), so the descriptor cannot be held ACROSS the publish;
// the binding it can express is:
//
//  1. verify with the handle OPEN (an open Go handle pins the name on
//     Windows — no renames are possible while we hold it);
//  2. land the times through the handle (SetFileTime), snapshot the
//     handle's identity (fstat volume+index), and close;
//  3. publish by path, then re-verify the destination against the
//     snapshot. A match is done: the close→publish window stayed clean.
//
// A MISMATCH — the window was used — is a typed refusal
// (ErrPublishStagedIdentityBreak): the plant never survives at a
// proven-absent destination (it is displaced), and the callers' kept+warn
// legs retain the genuine backup rather than consuming it. Unlike POSIX
// the genuine inode cannot be restaged (its surviving name is
// attacker-chosen and no descriptor stays open), so the recovery loop's
// guarantee degrades here to refusal-with-retention — still closing the
// finding's harm (attacker bytes installed silently, then the genuine
// backup consumed), just without silent self-heal.
func publishStagedBoundOS(p StagedPublish) error {
	of, _ := osStagingHandle(p.FS, p.Handle)
	handleInfo, verr := stagedIdentityProof(p.FS, p.Staged, p.Handle)
	if verr != nil {
		_ = p.Handle.Close()
		return fmt.Errorf("%w: %w", ErrPublishStagedVerify, verr)
	}
	if p.ApplyTimes {
		if terr := stagedHandleChtimes(of.Fd(), p.Atime, p.Mtime); terr != nil {
			if !errors.Is(terr, syscall.ENOSYS) {
				_ = p.Handle.Close()
				return &StagingTimesError{Staged: p.Staged, Err: terr}
			}
		}
	}
	if cerr := p.Handle.Close(); cerr != nil {
		return fmt.Errorf("%w: %s: %w", ErrPublishStagedClose, p.Staged, cerr)
	}
	if pubErr := p.Publish(p.FS, p.Staged, p.Dest); pubErr != nil {
		return pubErr
	}
	// The osStagingHandle gate above already proved the real OsFs — look the
	// destination up by name with os.Lstat directly (never follows a plant).
	destInfo, lerr := os.Lstat(p.Dest)
	switch {
	case lerr == nil && os.SameFile(handleInfo, destInfo):
		return nil
	case lerr != nil && !os.IsNotExist(lerr):
		return fmt.Errorf("post-publish reverify of %s: %w: %w", p.Dest, lerr, ErrPublishStagedIdentityBreak)
	case lerr == nil && p.NoReplace:
		// Proven-absent destination + successful no-replace publish + a
		// mismatched occupant: the occupant is the window plant this
		// publish itself installed — displace it, then refuse typed.
		_ = p.FS.Remove(p.Dest)
		return fmt.Errorf("publish of %s installed a substituted name (displaced): %w", p.Staged, ErrPublishStagedIdentityBreak)
	default:
		// Replace-style publish: the planted bytes are left in place under
		// the operation's replace semantics and the refusal is typed;
		// nothing genuine is consumed.
		return fmt.Errorf("publish of %s installed a substituted name: %w", p.Staged, ErrPublishStagedIdentityBreak)
	}
}
