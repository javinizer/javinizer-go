package downloader

// POSTER-WRITE-HARDENING wave-42 (codex P2, PR#215) — the downloader twin of
// history's finding: moveVerifiedRollbackBackupToQuarantine verified the
// zero-byte reservation and then ReplaceFile'd src onto it — REPLACING
// whatever occupied the name at rename time; a foreign plant landing between
// the verify and the rename had its bytes silently destroyed before the
// post-move re-verify could reject (the wave-32 fallback-handoff finding's
// history/rollback twin). The fix applies the EXACT conditional take-aside
// pattern (fsutil/bound_take.go's TakeAside shape — the construction
// backup_handoff.go's wave-38 fallback handoff added — mirrored identically
// into history's replacement_backup_quarantine.go):
//
//  1. the reservation placeholder is taken ASIDE onto a fresh O_EXCL-reserved
//     taken name (claimRollbackQuarantineName — crypto-token discipline
//     included) and the landed object is re-proven against the claim identity
//     at syscall adjacency;
//  2. ReplaceFile no longer moves src: the verified object records into the
//     provably-free reservation name via a NO-REPLACE rename
//     (fsutil.PublishNoReplace); a collision refuses typed with the plant
//     preserved and src untouched (it was never moved), the compensation
//     moving the placeholder back only when the name is free;
//  3. only the taken name is unlinked, after a final identity reproof —
//     the claimed placeholder is the sole thing this flow ever deletes.
//
// Test matrix mirrors history's wave-42 file leg for leg.

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w42RollbackHandoffFs replays the wave-42 handoff windows against the
// rollback quarantine. Wave-43: TakeAside's conditional handoff issues its
// own internal suffix renames (the reservation's vacate scratch→vac,
// compensating ride-backs, restores), so suffix-shape keying alone is
// ambiguous — the double LEARNS the two claimed names from the O_EXCL
// claims (claim 1 = the reservation name, claim 2 = the take-aside taken
// name; the transient ".vac." claim rides through): reservation→taken
// renames are the take-aside (it latches and fires the take hooks),
// backup→reservation renames are the publish, and lookups of the
// reservation name are counted once the take lands (lookup 1 = the freedom
// proof, lookup 2 = the no-replace publish's own classification or the
// restore's).
type w42RollbackHandoffFs struct {
	afero.Fs
	taken          bool
	quar           string // the reservation name = the publish target (learned: claim 1)
	takenName      string // the fresh O_EXCL-reserved take-aside name (learned: claim 2)
	claims         int
	onTakeBefore   func(oldname, newname string)
	onTakeAfter    func(oldname, newname string)
	proofScript    func() (os.FileInfo, error) // scripted answer for the freedom proof; nil → pass through
	afterProof     func()                      // fires after a REAL lookup-1 answer is computed (plant lands in the freedom→publish gap)
	beforeLookup2  func()                      // fires before the lookup-2 answer is computed (the vanish replay)
	publishErr     error                       // wedges the publish rename outright
	onPublishAfter func()                      // fires once the publish rename lands (the final-unlink window)
	lookups        int
}

func (f *w42RollbackHandoffFs) realLstat(name string) (os.FileInfo, bool, error) {
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f *w42RollbackHandoffFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.Contains(name, rollbackQuarantineSuffix) && !strings.Contains(name, ".vac.") {
		f.claims++
		switch f.claims {
		case 1:
			f.quar = name
		case 2:
			f.takenName = name
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *w42RollbackHandoffFs) Rename(oldname, newname string) error {
	switch {
	case oldname == f.quar && newname == f.takenName && !f.taken:
		// The take-aside: reservation name → taken name (wave-43: the
		// no-replace publish inside TakeAside).
		if f.onTakeBefore != nil {
			f.onTakeBefore(oldname, newname)
		}
		err := f.Fs.Rename(oldname, newname)
		if err == nil {
			f.taken = true
			if f.onTakeAfter != nil {
				f.onTakeAfter(oldname, newname)
			}
		}
		return err
	case newname == f.quar && !strings.Contains(oldname, rollbackQuarantineSuffix):
		// The publish: the verified object → the reservation name (the
		// restore leg taken→reservation rides through on the suffix gate).
		if f.publishErr != nil {
			return f.publishErr
		}
		err := f.Fs.Rename(oldname, newname)
		if err == nil && f.onPublishAfter != nil {
			f.onPublishAfter()
		}
		return err
	default:
		return f.Fs.Rename(oldname, newname)
	}
}

func (f *w42RollbackHandoffFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.taken && name == f.quar {
		f.lookups++
		switch f.lookups {
		case 1:
			if f.proofScript != nil {
				info, err := f.proofScript()
				return info, false, err
			}
			info, b, err := f.realLstat(name)
			if f.afterProof != nil {
				f.afterProof()
			}
			return info, b, err
		case 2:
			if f.beforeLookup2 != nil {
				f.beforeLookup2()
			}
		}
	}
	return f.realLstat(name)
}

// The headline leg: a foreign plant lands at the reservation name AFTER the
// caller's pre-move re-proof (rollbackQuarantineReservationStillOurs) and
// BEFORE the take-aside rename. The take moves the plant; the take-aside
// proof refuses it, it rides back onto the reservation name NO-REPLACE
// byte-intact, and the journaled backup never budges — where the pre-wave-42
// ReplaceFile would have overwritten the plant outright.
func TestRollbackBackupQuarantineW42_PlantAtReservationBetweenVerifyAndMoveRefused(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w42dh/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	plant := []byte("foreign plant swapped onto the reservation")
	fs := &w42RollbackHandoffFs{Fs: base}
	fs.onTakeBefore = func(oldname, _ string) {
		require.NoError(t, base.Remove(oldname))
		require.NoError(t, afero.WriteFile(base, oldname, plant, 0o600))
	}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w42 unit")
	require.Nil(t, hold)
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision,
		"a plant at the reservation name is the typed collision class")
	require.Contains(t, err.Error(), "not the claimed reservation placeholder")
	require.Contains(t, err.Error(), "take-aside of the quarantine reservation")
	require.True(t, fs.taken, "the take-aside rename ran over the plant")
	require.Equal(t, "old", string(readW31(t, base, backup)),
		"the journaled backup never moved — src and entry are unchanged")
	names := w32RollbackQuarNames(t, base, "/w42dh")
	require.Len(t, names, 1,
		"no stale taken names remain — the plant keeps the reservation name byte-intact")
	require.Equal(t, plant, mustReadDownloaderW7(t, base, "/w42dh/"+names[0]),
		"the foreign plant survives: where the pre-wave-42 ReplaceFile destroyed it, the wave-42 handoff preserves it")
	require.Equal(t, fs.quar, "/w42dh/"+names[0])
}

// A racer reclaims the freed reservation name between the take-aside and the
// source-freedom proof: typed collision, the plant preserved, BOTH foreign
// objects kept — the no-replace restore collides and strands only our own
// 0-byte placeholder at the taken name.
func TestRollbackBackupQuarantineW42_ReoccupiedAfterTakePreservesPlant(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w42dr/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	plant := []byte("racer reclaimed the freed reservation name")
	fs := &w42RollbackHandoffFs{Fs: base}
	fs.onTakeAfter = func(oldname, _ string) {
		require.NoError(t, afero.WriteFile(base, oldname, plant, 0o600))
	}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w42 unit")
	require.Nil(t, hold)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the re-occupancy is the typed collision class")
	require.ErrorIs(t, err, fsutil.ErrTakeAsideRestoreFailed,
		"the no-replace restore refused to clobber the racer's claimant")
	require.Contains(t, err.Error(), "re-occupied between the take-aside and the source-freedom proof")
	require.Equal(t, "old", string(readW31(t, base, backup)), "src untouched — never moved")
	require.Equal(t, plant, mustReadDownloaderW7(t, base, fs.quar), "the racer's plant is preserved byte-intact")
	names := w32RollbackQuarNames(t, base, "/w42dr")
	require.Len(t, names, 2, "the plant at the reservation name AND our stranded placeholder at the taken name")
	info, serr := base.Stat(fs.takenName)
	require.NoError(t, serr)
	require.Zero(t, info.Size(), "the stranded taken name holds only our 0-byte reservation placeholder")
}

// The re-occupancy claimant VANISHES again before the no-replace restore
// runs: the restore lands, the restored own placeholder is released by the
// identity-bound cleanup, and the name ends free for the retry.
func TestRollbackBackupQuarantineW42_ReoccupiedThenVanishedRestoresAndReleases(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w42drv/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	plant := []byte("transient racer")
	fs := &w42RollbackHandoffFs{Fs: base}
	fs.onTakeAfter = func(oldname, _ string) {
		require.NoError(t, afero.WriteFile(base, oldname, plant, 0o600))
	}
	fs.beforeLookup2 = func() {
		// The freedom proof (lookup 1) answered occupied; the claimant
		// vanishes before the restore's own classification (lookup 2).
		require.NoError(t, base.Remove(fs.quar))
	}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w42 unit")
	require.Nil(t, hold)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the freedom proof's occupancy is still the typed refusal")
	require.NotErrorIs(t, err, fsutil.ErrTakeAsideRestoreFailed,
		"the vanished claimant frees the name for the no-replace restore")
	require.Equal(t, "old", string(readW31(t, base, backup)))
	_, serr := base.Stat(fs.quar)
	require.ErrorIs(t, serr, os.ErrNotExist, "the restored own placeholder was released — the name ends free")
	require.Empty(t, w32RollbackQuarNames(t, base, "/w42drv"), "no litter survives the vanish leg")
}

// An indeterminate source-freedom proof refuses with the placeholder
// restored and released — plain error (never the collision class), the
// reservation name ends free.
func TestRollbackBackupQuarantineW42_IndeterminateFreedomProofRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w42di/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	sentinel := errors.New("w42 rollback freedom lookup wedged")
	fs := &w42RollbackHandoffFs{Fs: base}
	fs.proofScript = func() (os.FileInfo, error) { return nil, sentinel }

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w42 unit")
	require.Nil(t, hold)
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "indeterminate after the take-aside")
	require.NotErrorIs(t, err, fsutil.ErrPublishCollision,
		"an indeterminate answer proves nothing — never the typed collision class")
	require.Equal(t, "old", string(readW31(t, base, backup)))
	_, serr := base.Stat(fs.quar)
	require.ErrorIs(t, serr, os.ErrNotExist, "the restored placeholder was released — the name ends free")
	require.Empty(t, w32RollbackQuarNames(t, base, "/w42di"))
}

// A plant winning the freedom→publish gap: the freedom proof answered free,
// the publish's own no-replace classification finds the plant — typed
// refusal, the plant preserved, src NEVER moved (a failed publish relocates
// nothing), the stranded placeholder documented at the taken name.
func TestRollbackBackupQuarantineW42_PublishCollisionKeepsPlantAndSourceIntact(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w42dp/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	plant := []byte("plant winning the freedom-to-publish gap")
	fs := &w42RollbackHandoffFs{Fs: base}
	fs.afterProof = func() {
		require.NoError(t, afero.WriteFile(base, fs.quar, plant, 0o600))
	}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w42 unit")
	require.Nil(t, hold)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the no-replace publish reported the plant")
	require.ErrorIs(t, err, fsutil.ErrTakeAsideRestoreFailed, "the no-replace restore kept the plant byte-intact")
	require.Contains(t, err.Error(), "no-replace move of the verified object")
	require.Equal(t, "old", string(readW31(t, base, backup)),
		"src survives — the publish collided BEFORE anything was renamed")
	require.Equal(t, plant, mustReadDownloaderW7(t, base, fs.quar), "the gap plant is preserved byte-intact")
	names := w32RollbackQuarNames(t, base, "/w42dp")
	require.Len(t, names, 2, "plant at the reservation name + our stranded inert placeholder at the taken name")
	info, serr := base.Stat(fs.takenName)
	require.NoError(t, serr)
	require.Zero(t, info.Size())
}

// A plain publish wedge (kernel/IO, no collision): the placeholder rides
// back NO-REPLACE onto the free name and is released identity-bound — the
// reservation name ends free, src and entry unchanged.
func TestRollbackBackupQuarantineW42_PublishFailureRestoresAndReleases(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w42df/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	publishErr := errors.New("w42 rollback publish rename wedged")
	fs := &w42RollbackHandoffFs{Fs: base, publishErr: publishErr}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w42 unit")
	require.Nil(t, hold)
	require.ErrorIs(t, err, publishErr)
	require.NotErrorIs(t, err, fsutil.ErrPublishCollision, "a plain wedge relocates nothing and refuses plainly")
	require.NotErrorIs(t, err, fsutil.ErrTakeAsideRestoreFailed,
		"the reservation name was free — the restore landed")
	require.Equal(t, "old", string(readW31(t, base, backup)))
	require.Empty(t, w32RollbackQuarNames(t, base, "/w42df"),
		"placeholder restored + released: no .dlq. litter")
}

// The taken name VANISHES between the take-aside rename and its post-move
// re-proof: the owned placeholder went through a path this flow never
// unlinked — indeterminate (ErrTakeAsideVanished), nothing compensated
// (nothing sits at the name to move back), the freed reservation name needs
// no release.
func TestRollbackBackupQuarantineW42_TakenNameVanishedAfterTakeIsTyped(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w42dv/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	fs := &w42RollbackHandoffFs{Fs: base}
	fs.onTakeAfter = func(_, newname string) {
		require.NoError(t, base.Remove(newname))
	}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w42 unit")
	require.Nil(t, hold)
	require.ErrorIs(t, err, fsutil.ErrTakeAsideVanished)
	require.Contains(t, err.Error(), "take-aside of the quarantine reservation")
	require.Equal(t, "old", string(readW31(t, base, backup)), "the journaled backup never moved")
	_, serr := base.Stat(fs.quar)
	require.ErrorIs(t, serr, os.ErrNotExist, "the take freed the reservation name; the vanish released nothing further")
	require.Empty(t, w32RollbackQuarNames(t, base, "/w42dv"))
}

// A foreign writer swaps ITS object onto the freshly-claimed taken name
// before the take runs: the take-aside's scratch re-proof refuses typed
// (ErrTakeAsideForeign) before anything relocates — the reservation
// placeholder is released ours, the foreign scratch occupant preserved.
func TestRollbackBackupQuarantineW42_ScratchReservationSwapRefusedForeignPreserved(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w42ds/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	foreign := []byte("foreign occupant of the taken name")
	var takenName string
	fs := &w42RollbackOnCloseClaimFs{Fs: base, target: 2, onClose: func(name string) {
		takenName = name
		require.NoError(t, base.Remove(name))
		require.NoError(t, afero.WriteFile(base, name, foreign, 0o600))
	}}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w42 unit")
	require.Nil(t, hold)
	require.ErrorIs(t, err, fsutil.ErrTakeAsideForeign,
		"the taken-name scratch is re-proven before the move — a foreign swap refuses typed")
	require.NotErrorIs(t, err, fsutil.ErrPublishCollision)
	require.Equal(t, "old", string(readW31(t, base, backup)))
	require.Equal(t, foreign, mustReadDownloaderW7(t, base, takenName),
		"the foreign occupant of the taken name keeps its bytes byte-intact")
	names := w32RollbackQuarNames(t, base, "/w42ds")
	require.Len(t, names, 1, "only the preserved foreign occupant remains — our placeholder was released")
	require.Equal(t, takenName, "/w42ds/"+names[0])
}

// The final unlink's identity reproof catches a substitution at the taken
// name: ONLY the claimed placeholder is ever deleted, the foreign occupant
// is left byte-intact with a warn, and the handoff itself stands — the
// verified object is quarantined and unlinked normally.
func TestRollbackBackupQuarantineW42_FinalTakenUnlinkReproofMismatchPreservesOccupant(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w42du/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	occupant := []byte("foreign occupant at the taken name")
	fs := &w42RollbackHandoffFs{Fs: base}
	fs.onPublishAfter = func() {
		// A real foreign swap is remove+recreate: a bare WriteFile would
		// truncate MemMap's SAME FileData and mutate the held placeholder's
		// (live-view) FileInfo — the w35-documented MemMap hazard.
		require.NoError(t, base.Remove(fs.takenName))
		require.NoError(t, afero.WriteFile(base, fs.takenName, occupant, 0o600))
	}

	// The full flow SUCCEEDS: the inert-scratch refusal is warn-only.
	require.NoError(t, quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w42 unit"),
		"the handoff stands — only the taken-name release was refused")
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "the verified backup was removed normally")
	names := w32RollbackQuarNames(t, base, "/w42du")
	require.Len(t, names, 1, "only the preserved occupant lingers")
	require.Equal(t, fs.takenName, "/w42du/"+names[0])
	require.Equal(t, occupant, mustReadDownloaderW7(t, base, fs.takenName),
		"the final identity reproof preserved the foreign occupant — only the claimed placeholder is ever deleted")
}

// The taken-name claim itself can fail: the still-ours reservation
// placeholder is released identity-bound and nothing relocates.
func TestRollbackBackupQuarantineW42_TakeAsideClaimFailureReleasesReservation(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w42dc/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	sentinel := errors.New("w42 rollback reserve wedged")
	fs := &w42RollbackOnCloseClaimFs{Fs: base, target: 2, openErr: sentinel}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w42 unit")
	require.Nil(t, hold)
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "reserve the take-aside name")
	require.Equal(t, "old", string(readW31(t, base, backup)))
	require.Empty(t, w32RollbackQuarNames(t, base, "/w42dc"),
		"the still-ours reservation placeholder was released — nothing lingered")
}

// w42RollbackOnCloseClaimFs watches the Nth O_EXCL quarantine-name claim:
// swap on close (the claim→take handoff replay), or fail the reservation
// outright.
type w42RollbackOnCloseClaimFs struct {
	afero.Fs
	target  int // which O_EXCL claim to intercept (1 = the reservation, 2 = the taken name)
	onClose func(name string)
	openErr error
	seen    int
}

func (f *w42RollbackOnCloseClaimFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.Contains(name, rollbackQuarantineSuffix) {
		f.seen++
		if f.seen == f.target {
			if f.openErr != nil {
				return nil, f.openErr
			}
			file, err := f.Fs.OpenFile(name, flag, perm)
			if err != nil {
				return nil, err
			}
			return w42RollbackOnCloseFile{File: file, name: name, onClose: f.onClose}, nil
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// w42RollbackOnCloseFile runs a hook the moment the intercepted claim handle
// closes — the claim's last act before the caller's next step.
type w42RollbackOnCloseFile struct {
	afero.File
	name    string
	onClose func(name string)
}

func (f w42RollbackOnCloseFile) Close() error {
	err := f.File.Close()
	if err == nil && f.onClose != nil {
		f.onClose(f.name)
	}
	return err
}
