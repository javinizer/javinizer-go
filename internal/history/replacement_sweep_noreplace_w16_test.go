package history

// POSTER-WRITE-HARDENING codex PR#215 wave-16 (P2) — "publish
// missing-destination sweep restores without replacement": a sweep restore
// runs only after Lstat proves the destination ABSENT, but the pre-wave-16
// publish was a plain replace — a foreign writer claiming the destination in
// the classify→publish window was silently overwritten, then the backup was
// removed and the journal entry consumed, leaving the racer's bytes backed up
// nowhere. copyRestoreBytesNoReplace publishes through
// fsutil.PublishNoReplace now; the collision lands on the sweep's kept/warn
// legs (typed fsutil.ErrPublishCollision) with the racer intact, the backup
// retained, and the journal entry unconsumed. These tests replay the racer at
// the exact publish point (a rename hook stands in for renameat2 NOREPLACE /
// link(2) EEXIST / MoveFileEx-without-replace refusing the occupied name).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// w16SweepRacerFs claims the restore's destination inside the publish window:
// the first rename INTO dest (the staged ".rstr." publish) lands foreign
// bytes first and refuses exists-class, exactly what the platform no-replace
// primitive reports.
type w16SweepRacerFs struct {
	afero.Fs
	dest   string
	racer  []byte
	landed bool
}

func (f *w16SweepRacerFs) Rename(oldname, newname string) error {
	if !f.landed && filepath.Clean(newname) == filepath.Clean(f.dest) {
		f.landed = true
		if err := afero.WriteFile(f.Fs, f.dest, f.racer, 0o644); err != nil {
			return err
		}
		return &os.PathError{Op: "rename", Path: newname, Err: os.ErrExist}
	}
	return f.Fs.Rename(oldname, newname)
}

func w16NoStagedRestoreLeftovers(t *testing.T, fs afero.Fs, dir string) {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".rstr.", "the staged restore is cleaned up (saw %q)", e.Name())
	}
}

// Journaled crash-window restore colliding mid-publish: everything survives.
func TestSweepW16_JournaledMissingDestRestoreCollisionKeepsEverything(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dir := "/out/w16-sweep"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "pre-crash", time.Hour)
	op := journalRow(t, repo, "job-1", "W16-SWP", dest, backup, 1, models.RevertStatusApplied)

	fs := &w16SweepRacerFs{Fs: base, dest: dest, racer: []byte("racer bytes")}
	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed, "a collision restores nothing")
	require.True(t, fs.landed, "the injected race fired")

	require.Equal(t, "racer bytes", string(mustRead2(t, base, dest)),
		"the racer's bytes at the destination are never replaced")
	require.Equal(t, "pre-crash", string(mustRead2(t, base, backup)),
		"the journaled backup is retained")

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the journal entry is NOT consumed — a later retry arbitrates again")
	require.Equal(t, backup, gf.Replacements[0].Backup)

	w16NoStagedRestoreLeftovers(t, base, dir)
}

// The unjournaled (orphan) missing-destination restore collides the same way:
// the marker-shaped backup is retained for inspection, untouched.
func TestSweepW16_OrphanMissingDestRestoreCollisionKeepsEverything(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dir := "/out/w16-orphan"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "last-copy", time.Hour)
	// A sibling journal puts the directory in sweep space.
	journalRow(t, repo, "job-1", "W16-SIB", dir+"/other.jpg", dir+"/other.jpg.dlbak."+p3HexC, 1, models.RevertStatusApplied)

	fs := &w16SweepRacerFs{Fs: base, dest: dest, racer: []byte("racer bytes")}
	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed, "a collided orphan restore is retained, not reported healed")
	require.True(t, fs.landed)

	require.Equal(t, "racer bytes", string(mustRead2(t, base, dest)))
	require.Equal(t, "last-copy", string(mustRead2(t, base, backup)),
		"the unjournaled backup stays put for manual inspection")
	w16NoStagedRestoreLeftovers(t, base, dir)
}

// Control: the SAME missing-destination shape without a racer still restores
// and consumes (the wave-16 publish only adds the refusal, never a regression).
func TestSweepW16_JournaledMissingDestRestoreUnchangedWithoutRacer(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dir := "/out/w16-heal"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, backup, "pre-crash", time.Hour)
	op := journalRow(t, repo, "job-1", "W16-HEAL", dest, backup, 1, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "pre-crash", string(mustRead2(t, fs, dest)))
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists, "the consumed backup is removed")

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}
