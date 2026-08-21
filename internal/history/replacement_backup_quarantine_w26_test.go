package history

// POSTER-WRITE-HARDENING codex PR#215 wave-26 (P2, finding 4) — keep the
// backup identity bound THROUGH the unlink. Wave-25 verified the backup
// occupant dev/ino via an opened no-follow handle, CLOSED it, then unlinked
// by pathname — the substitution window simply shifted to just-before-unlink.
// The gate now quarantines-then-reverifies: the verified object is moved to a
// hard-to-guess O_EXCL-reserved quarantine name (handle still open on POSIX),
// re-proven against the verified snapshot at the quarantine name, and only
// the quarantine is unlinked. A substitution moved to the quarantine by the
// rename mismatches the re-verify (typed refusal, nothing removed); a plant
// racing onto the journaled name after the move keeps its bytes; a failed
// quarantine unlink compensates by moving the verified object back onto the
// journaled name (no-replace) so the armed entry retries from pre-state.
//
// Test matrix (unit legs run through the wave-32 successor chain
// quarantineReplacementBackupForRemoval + hold.removeVerified; the sweep leg
// pins the journal posture end-to-end):
//   - plant racing onto the journaled name after the move → plant SURVIVES,
//     quarantined verified object removed (OsFs, dev/ino binding)
//   - substitution inside the open→rename window → quarantined plant
//     mismatches the re-verify → typed refusal, plant moved back, never
//     unlinked
//   - re-verify legs on mem: indeterminate lookup / foreign metadata /
//     non-regular or nil answer / vanished-under-us / final unlink failure /
//     unlink already-removed / compensation collision keeps debris
//   - claim legs: entropy failure, occupied draw, racing reservation,
//     inspection failure, reservation failure/close failure, name exhaustion
//   - windows-posture seam: handle closed before the move, flow intact
//   - sweep integration: reverify mismatch → journal NOT consumed, entry
//     restore-pending, bytes moved back; wedge lifted → pending retry heals

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w26RenameHookFs replays the directory writer racing inside the removal
// gate's open→rename window: the hook fires exactly once, right BEFORE the
// quarantining rename delegates. Wave-42: the conditional handoff issues TWO
// suffix renames — the take-aside (suffix→suffix) and the publish (src→
// suffix); the hooks bind to the PUBLISH, the move of the journaled occupant.
type w26RenameHookFs struct {
	afero.Fs
	beforeMove func()
	afterMove  func()
}

func (f *w26RenameHookFs) Rename(oldname, newname string) error {
	if strings.Contains(newname, backupQuarantineSuffix) && !strings.Contains(oldname, backupQuarantineSuffix) {
		if f.beforeMove != nil {
			f.beforeMove()
		}
		err := f.Fs.Rename(oldname, newname)
		if err == nil && f.afterMove != nil {
			f.afterMove()
		}
		return err
	}
	return f.Fs.Rename(oldname, newname)
}

// The finding's headline case: the verified object is safely quarantined and
// removed while a foreign plant lands on the JOURNALED name mid-flow — the
// gate never removes the plant because only the quarantine name is unlinked.
func TestRemoveReplacementBackupW26_PlantAtJournaledNameSurvivesQuarantine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dev/inode identity and rename-over semantics are POSIX-shaped")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	backup := filepath.Join(tmp, "poster.jpg.dlbak."+p3HexA)
	require.NoError(t, os.WriteFile(backup, []byte("verified owned set-aside"), 0o640))

	fs := &w26RenameHookFs{Fs: base, afterMove: func() {
		// The racer plants foreign bytes on the journaled name between our
		// quarantine move and the final unlink — under wave-25 this is the
		// remove-by-pathname target.
		require.NoError(t, os.WriteFile(backup, []byte("foreign plant on the journaled name"), 0o644))
	}}

	require.NoError(t, quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil),
		"the verified object was quarantined + re-verified + removed — the removal itself succeeds")
	got, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "foreign plant on the journaled name", string(got),
		"the plant at the journaled name SURVIVES — never the quarantine unlink's target")
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".dlq.", "the quarantined verified object was removed")
	}
}

// A substitution INSIDE the open→rename window: the quarantining rename moves
// the attacker's plant instead of the verified object. The re-verify catches
// it (dev/inode mismatch), nothing is unlinked, and the compensation moves
// the quarantined plant back onto the journaled name — foreign bytes kept.
func TestRemoveReplacementBackupW26_SubstitutionBeforeMoveRefusesAndPreserves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dev/inode identity and rename-over semantics are POSIX-shaped")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	backup := filepath.Join(tmp, "poster.jpg.dlbak."+p3HexA)
	require.NoError(t, os.WriteFile(backup, []byte("verified owned set-aside"), 0o640))

	fs := &w26RenameHookFs{Fs: base, beforeMove: func() {
		require.NoError(t, os.Remove(backup))
		require.NoError(t, os.WriteFile(backup, []byte("foreign plant substituted before the move"), 0o644))
	}}

	err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
	var refused *BackupRemovalRefusedError
	require.ErrorAs(t, err, &refused,
		"the quarantined plant mismatches the re-verify — typed refusal, no removal")
	require.Contains(t, refused.Reason, "dev/inode mismatch")
	got, rerr := os.ReadFile(backup)
	require.NoError(t, rerr)
	require.Equal(t, "foreign plant substituted before the move", string(got),
		"the moved-back plant keeps its bytes at the journaled name — foreign objects are never unlinked by this gate")
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".dlq.", "the compensation moved the plant back — no quarantine debris")
	}
}

// w26QuarLstatFs rewrites post-move Lstat answers for quarantine names (the
// wedge flag flips on the successful quarantining PUBLISH rename — wave-42:
// the take-aside rename (suffix→suffix) no longer arms the seam, only the
// verified object's landing rename does — and only THE publish-target name is
// scripted), replaying rashly indeterminate or foreign re-verify answers.
// Claim-time lookups run before any move and pass through. disabled lifts
// the wedge for retry legs.
type w26QuarLstatFs struct {
	afero.Fs
	moved         bool
	quarName      string      // the publish target, learned at the publish rename (wave-42)
	info          os.FileInfo // substituted re-verify answer (takes precedence after err)
	err           error       // substituted lookup error (afero.ErrFileNotFound models the vanish leg)
	substituteNil bool        // answer (nil, nil) — the indeterminate nil arm
	disabled      bool
}

func (f *w26QuarLstatFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, backupQuarantineSuffix) && !strings.Contains(oldname, backupQuarantineSuffix) {
		f.moved = true
		f.quarName = newname
	}
	return err
}

func (f *w26QuarLstatFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.moved && !f.disabled && name == f.quarName {
		switch {
		case f.err != nil:
			return nil, false, f.err
		case f.substituteNil:
			return nil, false, nil
		default:
			return f.info, false, nil
		}
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// w26QuarGateFs stages the final-unlink legs: afterMove plants a foreign file
// on the journaled name after the quarantine PUBLISH move (wave-42: the
// take-aside rename never fires it), removeErr wedges the quarantine unlink
// itself, and notExist answers it already-removed. The wave-42 take-aside
// placeholder unlink (a 0-byte scratch with a warn-only wedge posture) is
// never the scripted victim — the wedge keys on the publish-target name.
type w26QuarGateFs struct {
	afero.Fs
	quarName  string
	afterMove func()
	removeErr error
	notExist  bool
}

func (f *w26QuarGateFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, backupQuarantineSuffix) && !strings.Contains(oldname, backupQuarantineSuffix) {
		f.quarName = newname
		if f.afterMove != nil {
			f.afterMove()
		}
	}
	return err
}

func (f *w26QuarGateFs) Remove(name string) error {
	// Wave-r19: the verified unlink runs the bound terminal unlink
	// (vacate→rebind→unlink terminal), so the scripted victim is the
	// object-bearing terminal Remove (a .dlq.-bearing name holding the
	// verified object after the vacate), not the vacated quarantine name.
	// The take-aside's 0-byte placeholder removes (warn-only) and the
	// bound-unlink's own 0-byte terminal-placeholder release fall through
	// (size 0); only the object-bearing remove carries the scripted arms.
	if strings.Contains(name, backupQuarantineSuffix) {
		if info, err := f.Fs.Stat(name); err == nil && info.Size() > 0 {
			if f.notExist {
				_ = f.Fs.Remove(name)
				return os.ErrNotExist
			}
			if f.removeErr != nil {
				return f.removeErr
			}
		}
	}
	return f.Fs.Remove(name)
}

func w26WriteBackup(t *testing.T, fs afero.Fs, backup, content string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(backup), 0o755))
	require.NoError(t, afero.WriteFile(fs, backup, []byte(content), 0o644))
}

func w26DirQuarNames(t *testing.T, fs afero.Fs, dir string) []string {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".dlq.") {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestRemoveReplacementBackupW26_ReverifyLegs(t *testing.T) {
	const backup = "/w26u/poster.jpg.dlbak." + p3HexA

	t.Run("indeterminate re-verify lookup keeps everything and restores pre-state", func(t *testing.T) {
		sentinel := errors.New("quarantine lstat wedged")
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarLstatFs{Fs: base, err: sentinel}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
		var refused *BackupRemovalRefusedError
		require.False(t, errors.As(err, &refused), "an indeterminate re-verify is a keep-error, not a foreign-occupant refusal")
		require.Equal(t, "old", string(mustRead2(t, base, backup)),
			"the compensation moved the quarantined object back onto the journaled name")
		require.Empty(t, w26DirQuarNames(t, base, "/w26u"), "no quarantine debris after the compensation")
	})

	t.Run("foreign metadata re-verify answer refuses typed and restores pre-state", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		require.NoError(t, afero.WriteFile(base, "/w26u/other", []byte("a much longer foreign file body"), 0o644))
		otherInfo, err := base.Stat("/w26u/other")
		require.NoError(t, err)
		fs := &w26QuarLstatFs{Fs: base, info: otherInfo}

		err = quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, err, &refused, "the quarantine-reverify mismatch leg refuses typed")
		require.Contains(t, refused.Reason, "metadata differs")
		require.Equal(t, "old", string(mustRead2(t, base, backup)), "moved back intact")
		require.Empty(t, w26DirQuarNames(t, base, "/w26u"))
	})

	t.Run("non-regular re-verify answer refuses typed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		require.NoError(t, base.MkdirAll("/w26u/dir", 0o755))
		dirInfo, err := base.Stat("/w26u/dir")
		require.NoError(t, err)
		fs := &w26QuarLstatFs{Fs: base, info: dirInfo}

		err = quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, err, &refused)
		require.Contains(t, refused.Reason, "not the verified regular file")
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
	})

	t.Run("nil re-verify answer refuses typed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarLstatFs{Fs: base, substituteNil: true}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, err, &refused)
		require.Contains(t, refused.Reason, "not the verified regular file")
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
	})

	t.Run("vanished under us is indeterminate retention, not consumption", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarLstatFs{Fs: base, err: afero.ErrFileNotFound}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		require.ErrorIs(t, err, errReplacementBackupQuarantineVanished,
			"wave-32 (finding R4): owned bytes that vanished unownably are never marked consumed")
		var refused *BackupRemovalRefusedError
		require.False(t, errors.As(err, &refused), "the vanished class is indeterminate retention, not a foreign-occupant refusal")
		_, statErr := base.Stat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist,
			"the move already relocated the object — the journaled name stays absent")
	})
}

func TestRemoveReplacementBackupW26_FinalUnlinkLegs(t *testing.T) {
	const backup = "/w26g/poster.jpg.dlbak." + p3HexA

	t.Run("wedged quarantine unlink compensates the move-back", func(t *testing.T) {
		sentinel := errors.New("quarantine unlink wedged")
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarGateFs{Fs: base, removeErr: sentinel}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "old", string(mustRead2(t, base, backup)),
			"the failed quarantine unlink moves the verified object back onto the journaled name")
		require.Empty(t, w26DirQuarNames(t, base, "/w26g"), "the compensation consumed the quarantine name")
	})

	t.Run("already-removed quarantine unlink is indeterminate retention", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarGateFs{Fs: base, notExist: true}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		require.ErrorIs(t, err, errReplacementBackupQuarantineVanished,
			"wave-32 (finding R4): ENOENT at Remove time means the owned bytes vanished unownably — not consumed")
		require.Empty(t, w26DirQuarNames(t, base, "/w26g"), "the object really is gone")
		_, statErr := base.Stat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("move-back collision keeps the verified object at the quarantine name", func(t *testing.T) {
		sentinel := errors.New("quarantine unlink wedged")
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarGateFs{
			Fs:        base,
			removeErr: sentinel,
			afterMove: func() {
				require.NoError(t, afero.WriteFile(base, backup, []byte("racer occupant on the journaled name"), 0o644))
			},
		}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		require.ErrorIs(t, err, sentinel, "the original unlink failure still surfaces")
		require.Equal(t, "racer occupant on the journaled name", string(mustRead2(t, base, backup)),
			"the NO-REPLACE compensation never clobbers the racer's occupant")
		require.Len(t, w26DirQuarNames(t, base, "/w26g"), 1,
			"the verified object stays recoverable at the quarantine name for manual recovery")
	})
}

// w26ErrReader wedges the quarantine token draw.
type w26ErrReader struct{ err error }

func (r w26ErrReader) Read([]byte) (int, error) { return 0, r.err }

// w26QuarClaimFs stages claim-window answers in draw order: occupied
// candidates, racing reservations, hard failures, and a failing reservation
// Close.
type w26QuarClaimFs struct {
	afero.Fs
	lstatOccupiedAnswers int
	lstatErr             error
	openExistAnswers     int
	openErr              error
	closeErr             error
}

func (f *w26QuarClaimFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if strings.Contains(name, backupQuarantineSuffix) {
		if f.lstatErr != nil {
			return nil, false, f.lstatErr
		}
		if f.lstatOccupiedAnswers > 0 {
			f.lstatOccupiedAnswers--
			info, err := f.Fs.Stat("/")
			return info, false, err
		}
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

type w26CloseFailFile struct {
	afero.File
	err error
}

func (f w26CloseFailFile) Close() error { return f.err }

func (f *w26QuarClaimFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(name, backupQuarantineSuffix) {
		if f.openErr != nil {
			return nil, f.openErr
		}
		if f.openExistAnswers > 0 {
			f.openExistAnswers--
			return nil, os.ErrExist
		}
		file, err := f.Fs.OpenFile(name, flag, perm)
		if err == nil && f.closeErr != nil {
			return w26CloseFailFile{File: file, err: f.closeErr}, nil
		}
		return file, err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func TestRemoveReplacementBackupW26_QuarantineClaimLegs(t *testing.T) {
	const backup = "/w26c/poster.jpg.dlbak." + p3HexA

	t.Run("entropy draw failure retains", func(t *testing.T) {
		sentinel := errors.New("entropy wedged")
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		prev := backupQuarantineRandReader
		backupQuarantineRandReader = w26ErrReader{err: sentinel}
		t.Cleanup(func() { backupQuarantineRandReader = prev })

		err := quarantineAndRemoveVerifiedReplacementBackup(base, backup, "w26 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
		require.Contains(t, err.Error(), "quarantine token")
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
	})

	t.Run("candidate inspection failure retains", func(t *testing.T) {
		sentinel := errors.New("quarantine lstat wedged")
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarClaimFs{Fs: base, lstatErr: sentinel}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
		require.Contains(t, err.Error(), "inspect quarantine candidate")
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
	})

	t.Run("occupied first draw climbs to the next candidate", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarClaimFs{Fs: base, lstatOccupiedAnswers: 1}

		require.NoError(t, quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil),
			"the second draw claims and the removal completes")
		require.Empty(t, w26DirQuarNames(t, base, "/w26c"))
	})

	t.Run("racing reservation on the first draw climbs", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarClaimFs{Fs: base, openExistAnswers: 1}

		require.NoError(t, quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil))
		require.Empty(t, w26DirQuarNames(t, base, "/w26c"))
	})

	t.Run("reservation failure retains", func(t *testing.T) {
		sentinel := errors.New("quarantine reserve wedged")
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarClaimFs{Fs: base, openErr: sentinel}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
		require.Contains(t, err.Error(), "reserve quarantine candidate")
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
	})

	t.Run("reservation close failure drops the placeholder", func(t *testing.T) {
		sentinel := errors.New("reservation close wedged")
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarClaimFs{Fs: base, closeErr: sentinel}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
		require.Contains(t, err.Error(), "close quarantine reservation")
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
		require.Empty(t, w26DirQuarNames(t, base, "/w26c"), "the unknown-state placeholder was dropped")
	})

	t.Run("quarantine name exhaustion refuses", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26QuarClaimFs{Fs: base, lstatOccupiedAnswers: backupQuarantineClaimTries}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "quarantine names exhausted")
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
	})

	t.Run("quarantine move failure keeps the journal name untouched", func(t *testing.T) {
		sentinel := errors.New("quarantine move wedged")
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w26MoveFailFs{Fs: base, err: sentinel}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w26 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "old", string(mustRead2(t, base, backup)),
			"a failed move relocated nothing — the journaled occupant stays put")
		require.Empty(t, w26DirQuarNames(t, base, "/w26c"), "the reservation placeholder was cleaned")
	})
}

// w26MoveFailFs wedges the quarantining rename itself.
type w26MoveFailFs struct {
	afero.Fs
	err error
}

func (f *w26MoveFailFs) Rename(oldname, newname string) error {
	if strings.Contains(newname, backupQuarantineSuffix) {
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

// The Windows-posture seam closes the no-follow handle BEFORE the quarantine
// move (MoveFileEx cannot rename an open Go handle); the re-verify still
// binds the moved object, and the flow completes through the replace-aware
// rename over the reservation placeholder.
func TestRemoveReplacementBackupW26_WindowsPostureSeam(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w26w/poster.jpg.dlbak." + p3HexA
	w26WriteBackup(t, base, backup, "old")

	prev := fsutil.PathBackslashesAreSeparators
	fsutil.PathBackslashesAreSeparators = true
	t.Cleanup(func() { fsutil.PathBackslashesAreSeparators = prev })

	require.NoError(t, quarantineAndRemoveVerifiedReplacementBackup(base, backup, "w26 unit", nil, nil))
	exists, _ := afero.Exists(base, backup)
	require.False(t, exists, "the handle-close-first windows posture completes the removal")
	require.Empty(t, w26DirQuarNames(t, base, "/w26w"))
}

// Sweep integration: a reverify mismatch inside the crash-window consumption
// keeps the journal entry LIVE (restore-pending), the restored destination
// stays, and the verified object moves back onto the journaled name — then,
// wedge lifted, the pending retry heals the armed state cleanly.
func TestSweepW26_QuarantineReverifyMismatchRetainsEntryAndRetryHeals(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/w26s/other.bin", []byte("a mismatch-sized foreign body"), config.FilePerm))
	otherInfo, err := base.Stat("/w26s/other.bin")
	require.NoError(t, err)
	fs := &w26QuarLstatFs{Fs: base, info: otherInfo}
	repo := newP3OpRepo()
	op, dest, backup := w25SweepCrashOp(t, fs, repo, "W26Q", []byte("original bytes"), "stamped")

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Zero(t, healed, "the mismatched quarantine re-verify is never consumed")
	require.Equal(t, "original bytes", string(mustRead2(t, base, dest)),
		"the crash-window restore landed")
	require.Equal(t, "original bytes", string(mustRead2(t, base, backup)),
		"the compensation moved the verified object back onto the journaled name")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "the journal entry was NOT consumed on the mismatched leg")
	require.True(t, entries[0].RestorePending, "the pending retry marker persists")
	require.Empty(t, w26DirQuarNames(t, base, filepath.Dir(backup)), "no quarantine debris")

	// Wedge lifted: the pending retry (present destination + committed marker)
	// re-runs the removal binding cleanly and consumes.
	fs.moved = false
	fs.info = nil
	fs.disabled = true
	healed, err = NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed, "the clean-kind pending retry removes + consumes")
	exists, _ := afero.Exists(base, backup)
	require.False(t, exists)
	require.Empty(t, w25JournalEntries(t, repo, op.ID))
}
