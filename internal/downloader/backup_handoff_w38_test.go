package downloader

// POSTER-WRITE-HARDENING wave-38 (codex P2, PR#215 finding F2) — the
// fallback reserved-backup handoff (no renameat2) is CONDITIONAL: the
// reservation placeholder is taken ASIDE onto a bound scratch first (proof:
// the backup name must be free afterwards AND the taken-aside object must
// still be the claim's identity), dest moves onto the freed name NO-REPLACE,
// and only the scratch is ever unlinked (re-bound at unlink time). These
// legs replay every wedge ordering on a virtual filesystem.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w38Pair builds dest + a claimed 0-byte reservation pair.
func w38Pair(t *testing.T, base afero.Fs, dir string) (dest, backup string, claim os.FileInfo) {
	t.Helper()
	require.NoError(t, base.MkdirAll(dir, 0o755))
	dest = filepath.Join(dir, "poster.jpg")
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	var err error
	backup, claim, err = claimOverwriteBackupPath(base, dest, "w38-op")
	require.NoError(t, err)
	return dest, backup, claim
}

// The conditional order: take aside → dest moves no-replace → scratch
// unlinked. The placeholder never lingers and no foreign name is touched.
func TestHandoffViaVerifiedRenameW38_HappyPath(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/w38-happy"
	dest, backup, claim := w38Pair(t, base, dir)

	require.NoError(t, handoffViaVerifiedRename(base, dest, backup, claim))
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, backup)),
		"the destination bytes landed at the reserved name")
	_, serr := base.Stat(dest)
	require.ErrorIs(t, serr, os.ErrNotExist, "the destination name is free for the staged publish")
	entries, err := afero.ReadDir(base, dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotContains(t, e.Name(), rollbackQuarantineSuffix, "no scratch litter remains")
	}
}

// A scratch that cannot be claimed wedges the handoff BEFORE anything moves;
// the still-ours reservation is released by the wave-37 binding.
func TestHandoffViaVerifiedRenameW38_ScratchClaimFailureReleasesReservation(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup, claim := w38Pair(t, base, "/out/w38-claimfail")
	sentinel := errors.New("w38 entropy wedged")
	prev := rollbackQuarantineRandReader
	rollbackQuarantineRandReader = &w37xFailReader{err: sentinel}
	t.Cleanup(func() { rollbackQuarantineRandReader = prev })

	err := handoffViaVerifiedRename(base, dest, backup, claim)
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "take-aside scratch claim")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "the still-ours reservation was released")
}

// The mid-take swap: a racer's object at the reservation name is what the
// take moves aside — the proof refuses, the racer's object rides back onto
// the reservation name NO-REPLACE, byte-intact; destination untouched.
func TestHandoffViaVerifiedRenameW38_TakeMovedForeignObjectRestored(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup, claim := w38Pair(t, base, "/out/w38-takeswap")
	fs := &w37xDestSwapOnTakeFs{Fs: base, dest: backup}

	err := handoffViaVerifiedRename(fs, dest, backup, claim)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the take-aside proof refused the foreign object")
	require.ErrorContains(t, err, "take-aside of the reservation")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	require.Equal(t, "racer pre-take swap", string(mustReadDownloaderW7(t, base, backup)),
		"the racer's occupant moved back onto the reservation name byte-intact — never displaced, never unlinked")
}

// A racer reclaims the freed reservation name between the take and the
// source-freedom proof: typed collision refusal; the plant is preserved; the
// no-replace restore collides and the placeholder stays at the scratch name.
func TestHandoffViaVerifiedRenameW38_ReoccupiedAfterTakePreservesPlant(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/w38-reoccupied"
	dest, backup, claim := w38Pair(t, base, dir)
	plant := []byte("racer reclaimed the freed name")
	fs := &w38PlantAfterTakeFs{Fs: base, takeSrc: backup, plantPath: backup, plant: plant}

	err := handoffViaVerifiedRename(fs, dest, backup, claim)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the re-occupancy is the typed collision class")
	require.ErrorIs(t, err, fsutil.ErrTakeAsideRestoreFailed,
		"the no-replace restore refused to clobber the racer's claimant")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	require.Equal(t, plant, mustReadDownloaderW7(t, base, backup), "the racer's plant is preserved byte-intact")
	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	var scratches []string
	for _, e := range entries {
		if strings.Contains(e.Name(), rollbackQuarantineSuffix) {
			scratches = append(scratches, e.Name())
		}
	}
	require.Len(t, scratches, 1, "our placeholder stays recoverable at the scratch name")
	info, serr := base.Stat(filepath.Join(dir, scratches[0]))
	require.NoError(t, serr)
	require.Zero(t, info.Size(), "the scratch holds the 0-byte reservation placeholder")
}

// The re-occupancy proof fires but the claimant VANISHES again before the
// no-replace restore runs: the restore succeeds, and the restored own
// placeholder is released by the wave-37 binding (the name ends free).
func TestHandoffViaVerifiedRenameW38_ReoccupiedThenVanishedRestoredAndReleased(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup, claim := w38Pair(t, base, "/out/w38-reoccupied-vanished")
	plant := []byte("transient racer")
	fs := &w38PlantThenVanishAfterTakeFs{Fs: base, takeSrc: backup, plantPath: backup, plant: plant}

	err := handoffViaVerifiedRename(fs, dest, backup, claim)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the freedom-proof occupancy is still the typed refusal")
	require.NotErrorIs(t, err, fsutil.ErrTakeAsideRestoreFailed,
		"the vanished claimant frees the name for the no-replace restore")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist,
		"the restored own placeholder was released — the name ends free for the retry")
}

// An indeterminate source-freedom proof refuses with the placeholder
// restored and released — the reservation name ends free again.
func TestHandoffViaVerifiedRenameW38_IndeterminateFreedomProofRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup, claim := w38Pair(t, base, "/out/w38-freedom-indet")
	sentinel := errors.New("w38 freedom lookup wedged")
	fs := &w38FailFreedomStatAfterTakeFs{Fs: base, takeSrc: backup, err: sentinel}

	err := handoffViaVerifiedRename(fs, dest, backup, claim)
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "indeterminate after the take-aside")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist,
		"the restored placeholder was released again — the name ends free for the retry")
}

// The dest→backup move wedges (kernel error replayed through a rename seam
// — the plant is PRESERVED, never overwritten) and the placeholder rides
// back + is released by the wave-37 binding.
func TestHandoffViaVerifiedRenameW38_NoReplaceMoveCollisionKeepsPlant(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/w38-move-collision"
	dest, backup, claim := w38Pair(t, base, dir)
	plant := []byte("racer between the freedom proof and the move")
	fs := &w38PlantOnPublishClassifyFs{Fs: base, takeSrc: backup, plantPath: backup, plant: plant}

	err := handoffViaVerifiedRename(fs, dest, backup, claim)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the publish reported the racer — the collision class surface")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	require.Equal(t, plant, mustReadDownloaderW7(t, base, backup),
		"the move-window racer was never renamed over; it keeps the name")
}

// The dest→backup move wedged outright (I/O): same restore+release tail.
func TestHandoffViaVerifiedRenameW38_MoveFailureRestoresAndReleases(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup, claim := w38Pair(t, base, "/out/w38-move-fail")
	sentinel := errors.New("w38 handed move wedged")
	fs := &w38FailDestPublishFs{Fs: base, moveSrc: dest, err: sentinel}

	err := handoffViaVerifiedRename(fs, dest, backup, claim)
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "no-replace move of the destination")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)), "destination untouched")
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist,
		"the restored placeholder was released — no reservation litter survives")
}

// A wedged scratch unlink after the successful handoff leaves inert litter
// and WARNS — the handoff itself stands (dest freed, backup holds bytes).
func TestHandoffViaVerifiedRenameW38_ScratchUnlinkWedgeLeavesInertLitter(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/w38-unlink-wedge"
	dest, backup, claim := w38Pair(t, base, dir)
	fs := &w37RemoveFailFs{Fs: base, victim: rollbackQuarantineSuffix, err: errors.New("w38 scratch unlink wedged")}

	require.NoError(t, handoffViaVerifiedRename(fs, dest, backup, claim),
		"the handoff stands — the bound unlink wedge is warn-only litter")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, backup)))
	_, serr := base.Stat(dest)
	require.ErrorIs(t, serr, os.ErrNotExist)
	entries, rerr := afero.ReadDir(base, dir)
	require.NoError(t, rerr)
	var scratches []string
	for _, e := range entries {
		if strings.Contains(e.Name(), rollbackQuarantineSuffix) {
			scratches = append(scratches, e.Name())
		}
	}
	require.Len(t, scratches, 1, "the taken-aside placeholder lingers as inert litter")
}

// --- wedge doubles ---------------------------------------------------------

// w38PlantAfterTakeFs plants at plantPath right after the take-aside rename
// (takeSrc→scratch) succeeds — the re-occupancy mid-window replay.
type w38PlantAfterTakeFs struct {
	afero.Fs
	takeSrc   string
	plantPath string
	plant     []byte
	done      bool
}

func (f *w38PlantAfterTakeFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && !f.done && oldname == f.takeSrc && strings.Contains(newname, rollbackQuarantineSuffix) {
		f.done = true
		if perr := afero.WriteFile(f.Fs, f.plantPath, f.plant, 0o600); perr != nil {
			return perr
		}
	}
	return err
}

// w38FailFreedomStatAfterTakeFs answers indeterminately at the FIRST lookup
// of the source name after the take-aside rename (the freedom proof).
type w38FailFreedomStatAfterTakeFs struct {
	afero.Fs
	takeSrc string
	err     error
	taken   bool
	fired   bool
}

func (f *w38FailFreedomStatAfterTakeFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && oldname == f.takeSrc && strings.Contains(newname, rollbackQuarantineSuffix) {
		f.taken = true
	}
	return err
}

func (f *w38FailFreedomStatAfterTakeFs) Stat(name string) (os.FileInfo, error) {
	if f.taken && !f.fired && name == f.takeSrc {
		f.fired = true
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

// w38PlantOnPublishClassifyFs plants at plantPath at the SECOND post-take
// lookup of the name — the freedom proof (first) answers free, then the
// racer claims it before the no-replace move's own classification.
type w38PlantOnPublishClassifyFs struct {
	afero.Fs
	takeSrc   string
	plantPath string
	plant     []byte
	taken     bool
	armed     int
}

func (f *w38PlantOnPublishClassifyFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && oldname == f.takeSrc && strings.Contains(newname, rollbackQuarantineSuffix) {
		f.taken = true
	}
	return err
}

func (f *w38PlantOnPublishClassifyFs) Stat(name string) (os.FileInfo, error) {
	if f.taken && name == f.plantPath {
		f.armed++
		if f.armed == 2 {
			if perr := afero.WriteFile(f.Fs, f.plantPath, f.plant, 0o600); perr != nil {
				return nil, perr
			}
		}
	}
	return f.Fs.Stat(name)
}

// w38PlantThenVanishAfterTakeFs plants at plantPath at the take-aside
// instant and removes the plant again at the SECOND lookup of the name — the
// freedom proof (first lookup) sees the occupant; the restore's own
// classification (second lookup) finds the name free again.
type w38PlantThenVanishAfterTakeFs struct {
	afero.Fs
	takeSrc   string
	plantPath string
	plant     []byte
	taken     bool
	lookups   int
}

func (f *w38PlantThenVanishAfterTakeFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && !f.taken && oldname == f.takeSrc && strings.Contains(newname, rollbackQuarantineSuffix) {
		f.taken = true
		if perr := afero.WriteFile(f.Fs, f.plantPath, f.plant, 0o600); perr != nil {
			return perr
		}
	}
	return err
}

func (f *w38PlantThenVanishAfterTakeFs) Stat(name string) (os.FileInfo, error) {
	if f.taken && name == f.plantPath {
		f.lookups++
		if f.lookups == 2 {
			// The freedom proof (first lookup) answered occupied; the claimant
			// vanishes before the restore's own classification (second lookup).
			if err := f.Fs.Remove(name); err != nil {
				return nil, err
			}
		}
	}
	return f.Fs.Stat(name)
}

// w38FailDestPublishFs fails the dest rename (the no-replace move), letting
// the take-aside (backup name → scratch) proceed normally.
type w38FailDestPublishFs struct {
	afero.Fs
	moveSrc string
	err     error
}

func (f *w38FailDestPublishFs) Rename(oldname, newname string) error {
	if oldname == f.moveSrc {
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}
