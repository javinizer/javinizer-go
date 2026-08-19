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
// (ErrPublishStagedIdentityBreak), and the callers' kept+warn legs retain
// the genuine backup rather than consuming it. Unlike POSIX the genuine
// inode cannot be restaged (its surviving name is attacker-chosen and no
// descriptor stays open), so the recovery loop's guarantee degrades here
// to refusal-with-retention — still closing the finding's harm (attacker
// bytes installed silently, then the genuine backup consumed), just
// without silent self-heal.
//
// Wave-38 (codex P2, PR#215 finding F1) mirrors the POSIX occupancy-tie
// rule here: the post-publish mismatch occupant is NEVER unlinked. A
// successful no-replace publish proved the destination free at the move
// instant, so anything landing there afterwards — the staged-name plant
// the publish moved over or a legitimate file created inside the
// close→publish→reverify window — is indistinguishable at this layer and
// is preserved byte-intact; every mismatch arm refuses typed with nothing
// removed.
func publishStagedBoundOS(p StagedPublish) (os.FileInfo, error) {
	of, _ := osStagingHandle(p.FS, p.Handle)
	handleInfo, verr := stagedIdentityProof(p.FS, p.Staged, p.Handle)
	if verr != nil {
		_ = p.Handle.Close()
		return nil, fmt.Errorf("%w: %w", ErrPublishStagedVerify, verr)
	}
	if p.ApplyTimes {
		if terr := stagedHandleChtimes(of.Fd(), p.Atime, p.Mtime); terr != nil {
			if !errors.Is(terr, syscall.ENOSYS) {
				_ = p.Handle.Close()
				return nil, &StagingTimesError{Staged: p.Staged, Err: terr}
			}
		}
	}
	if cerr := p.Handle.Close(); cerr != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrPublishStagedClose, p.Staged, cerr)
	}
	if pubErr := p.Publish(p.FS, p.Staged, p.Dest); pubErr != nil {
		return nil, pubErr
	}
	// The osStagingHandle gate above already proved the real OsFs — look the
	// destination up by name through the Lstat seam (production: os.Lstat,
	// never follows a plant).
	destInfo, lerr := publishStagedBoundDestLstat(p.Dest)
	switch {
	case lerr == nil && os.SameFile(handleInfo, destInfo):
		return destInfo, nil
	case lerr != nil && !os.IsNotExist(lerr):
		return nil, fmt.Errorf("post-publish reverify of %s: %w: %w", p.Dest, lerr, ErrPublishStagedIdentityBreak)
	case lerr == nil && p.NoReplace:
		// Proven-absent destination + successful no-replace publish + a
		// mismatched occupant (destInfo, recorded at the detection instant):
		// wave-38 (codex P2, PR#215 finding F1) preserves the occupant
		// byte-intact on EVERY arm — no pre-publish existence evidence can
		// ever tie it (the publish proved the destination free), and the
		// pre-wave-38 record-then-displace binding deleted legitimate
		// window files alongside plants. The binding re-lookup runs only to
		// keep the refusal's diagnostics honest; it never authorizes a
		// removal.
		_, oerr := publishStagedBoundDestLstat(p.Dest)
		switch {
		case oerr != nil && !os.IsNotExist(oerr):
			return nil, fmt.Errorf("post-publish occupant binding lookup of %s: %w: %w", p.Dest, oerr, ErrPublishStagedIdentityBreak)
		default:
			return nil, fmt.Errorf("%w at %s — publish of %s installed a substituted name; post-publish occupant preserved (never displaced): %w", ErrPublishStagedForeignOccupant, p.Dest, p.Staged, ErrPublishStagedIdentityBreak)
		}
	default:
		// Replace-style publish: the planted bytes are left in place under
		// the operation's replace semantics and the refusal is typed;
		// nothing genuine is consumed.
		return nil, fmt.Errorf("publish of %s installed a substituted name: %w", p.Staged, ErrPublishStagedIdentityBreak)
	}
}
