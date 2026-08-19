package history

// POSTER-WRITE-HARDENING wave-15 (codex P2) — "publish re-armed backups
// without replacing collisions": the re-arm swap (staged copy → backup) used
// to be a plain Rename, so a foreign file claiming the backup name mid-window
// (the caller has just REMOVED the original backup, widening the window) was
// silently destroyed. copyRearmSourceBytes now publishes through
// fsutil.PublishNoReplace: a collision drops the staged copy and reports the
// typed fsutil.ErrPublishCollision class through rearmReplacementBackup,
// leaving the foreign bytes intact (every re-arm caller treats a failed
// re-arm as kept+warn; round 18 (codex P2) then refines the caller side so
// an entry whose re-arm was refused with an occupied-name class is marked
// RestorePending instead of staying armed against the occupant).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w15BackupRaceFs claims the backup name with FOREIGN bytes inside the
// copy→publish window and then refuses the rename the way the platform's
// no-replace primitive does — renameat2(RENAME_NOREPLACE) on Linux,
// MoveFileExW without MOVEFILE_REPLACE_EXISTING on Windows, link(2)'s EEXIST
// elsewhere. Fires exactly_once; every other rename forwards.
type w15BackupRaceFs struct {
	afero.Fs
	target  string
	foreign []byte
	fired   bool
}

func (f *w15BackupRaceFs) Rename(oldname, newname string) error {
	if !f.fired && filepath.Clean(newname) == filepath.Clean(f.target) {
		f.fired = true
		if err := afero.WriteFile(f.Fs, f.target, f.foreign, 0o600); err != nil {
			return err
		}
		return &os.PathError{Op: "rename", Path: newname, Err: os.ErrExist}
	}
	return f.Fs.Rename(oldname, newname)
}

func w15DirListing(t *testing.T, fs afero.Fs, dir string) []string {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// The collision unit: the re-arm publish must refuse the foreign-occupied
// backup name through the typed collision class, drop its staged copy, and
// keep every byte where it was.
func TestW15RearmPublishCollisionPreservesForeignBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/W15-COLLIDE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak.a"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current-bytes"), 0o640))

	fs := &w15BackupRaceFs{Fs: base, target: backup, foreign: []byte("foreign-bytes")}
	err := rearmReplacementBackup(fs, dest, backup, nil)
	require.Error(t, err, "a mid-window foreign claim fails the re-arm")
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the failure carries the typed collision class")
	require.Contains(t, err.Error(), "re-arm install backup")
	require.True(t, fs.fired, "the injected race fired")

	require.Equal(t, "foreign-bytes", string(mustReadHistoryW15(t, base, backup)),
		"the foreign file at the backup name is untouched")
	require.Equal(t, "current-bytes", string(mustReadHistoryW15(t, base, dest)),
		"the re-arm source is untouched")
	for _, name := range w15DirListing(t, base, dir) {
		require.NotContains(t, name, rearmStagingSuffix+".", "the staged re-arm copy is cleaned up (saw %q)", name)
	}
}

// An ALREADY-occupied backup name (no race window at all) collides on the
// publish's classification leg with the same preserved-bytes posture.
func TestW15RearmPublishOccupiedAtClassifyPreservesForeignBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/W15-OCCUPIED"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak.a"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current-bytes"), 0o640))
	require.NoError(t, afero.WriteFile(base, backup, []byte("foreign-bytes"), 0o600))

	err := rearmReplacementBackup(base, dest, backup, nil)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision)
	require.Equal(t, "foreign-bytes", string(mustReadHistoryW15(t, base, backup)))
	require.Equal(t, "current-bytes", string(mustReadHistoryW15(t, base, dest)))
	for _, name := range w15DirListing(t, base, dir) {
		require.NotContains(t, name, rearmStagingSuffix+".", "the staged re-arm copy is cleaned up")
	}
}

// The normal re-arm is unchanged: dest bytes land at the backup with the
// source's permission bits and timestamps carried.
func TestW15RearmNormalPublishUnchanged(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/W15-NORMAL"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak.a"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current-bytes"), 0o640))
	info, err := base.Stat(dest)
	require.NoError(t, err)

	require.NoError(t, rearmReplacementBackup(base, dest, backup, info))
	require.Equal(t, "current-bytes", string(mustReadHistoryW15(t, base, backup)))
	gotInfo, err := base.Stat(backup)
	require.NoError(t, err)
	require.Equal(t, info.Mode().Perm(), gotInfo.Mode().Perm(), "the re-armed backup carries the original permission bits")
	require.Equal(t, "current-bytes", string(mustReadHistoryW15(t, base, dest)), "the re-arm is a copy, never a move")
	require.Len(t, w15DirListing(t, base, dir), 2, "dest + backup only — no staged residue")
}

// w15ConsumeFailRepo fails the NEXT journal transaction, mimicking the
// consumption commit failure that sends restoreReplacementJournal into its
// re-arm compensation.
type w15ConsumeFailRepo struct {
	database.BatchFileOperationRepositoryInterface
	failNext bool
}

func (m *w15ConsumeFailRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if m.failNext {
		m.failNext = false
		return errors.New("w15 consumption transaction wedged")
	}
	return m.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, fn)
}

// End-to-end through the reverter's consume-failure compensation: the restore
// lands, the consumption fails, the re-arm collides with the foreign claim —
// and the sweep-facing posture is exactly the pre-fix kept path: the error
// surfaces, the journal entry is kept for a retry (round 18: marked
// RestorePending, never left armed against the occupant), and the foreign
// bytes survive.
func TestW15ReverterRearmCollisionKeepsArmedPostureAndForeignBytes(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w15-rearm", "W15-REARM", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	race := &w15BackupRaceFs{Fs: fixture.fs, target: backup, foreign: []byte("foreign-bytes")}
	repo := &w15ConsumeFailRepo{BatchFileOperationRepositoryInterface: fixture.repo, failNext: true}

	_, err := NewReverter(race, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err, "the consumption failure still surfaces as the revert error")
	require.Contains(t, err.Error(), "journal consumption failed")
	require.Contains(t, err.Error(), "w15 consumption transaction wedged")
	require.True(t, race.fired, "the re-arm raced the foreign claim")

	require.Equal(t, "foreign-bytes", string(p3ReadFile(t, fixture.fs, backup)),
		"the foreign file at the backup name is untouched by the re-arm")
	require.Equal(t, "old", string(p3ReadFile(t, fixture.fs, dest)),
		"the restore itself landed (it precedes the consumption failure)")

	row, ferr := fixture.repo.FindByID(context.Background(), op.ID)
	require.NoError(t, ferr)
	gf, perr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, perr)
	require.Len(t, gf.Replacements, 1, "the failed consumption keeps the journal entry (the kept posture)")
	require.Equal(t, backup, gf.Replacements[0].Backup)
	require.True(t, gf.Replacements[0].RestorePending,
		"round 18 (codex P2): after a collided re-arm the entry is marked restore-pending — never left armed against the foreign occupant")

	for _, name := range w15DirListing(t, fixture.fs, filepath.Dir(dest)) {
		require.False(t, strings.Contains(name, rearmStagingSuffix+"."), "no staged re-arm residue (saw %q)", name)
	}
	_, markerErr := fixture.fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist, "the destination busy marker is released")
}

func mustReadHistoryW15(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return data
}
