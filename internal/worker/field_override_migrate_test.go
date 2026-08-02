package worker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrateFailFs injects targeted failures into an underlying in-memory fs so
// MigratePosterCacheAssets' forward-move and snapshot-reversal legs become
// exercisable deterministically (mirrors the poster package's moveFailFs).
// removeErr fails Remove on exact paths (the non-empty-directory/EPERM
// blocker class); renameErr fails Rename keyed by the OLD path; writeErr fails
// write-mode OpenFile on exact paths while writeErrOn is set (WriteFile
// during a snapshot restore). Reads (O_RDONLY) are never affected.
type migrateFailFs struct {
	afero.Fs
	removeErr  map[string]error
	renameErr  map[string]error
	writeErrOn bool
	writeErr   map[string]error
}

func (f *migrateFailFs) Remove(name string) error {
	if err, ok := f.removeErr[name]; ok {
		return err
	}
	return f.Fs.Remove(name)
}

func (f *migrateFailFs) Rename(oldname, newname string) error {
	if err, ok := f.renameErr[oldname]; ok {
		return err
	}
	return f.Fs.Rename(oldname, newname)
}

func (f *migrateFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.writeErrOn && flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		if err, ok := f.writeErr[name]; ok {
			return nil, err
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// migrateFixture builds a ScrapePosterGenerator backed by a REAL
// PosterManager over the given fs, with both origin assets present, and
// returns the generator plus the job's poster directory. Fixture files are
// written through fs so injected failures cannot leak into the setup.
func migrateFixture(t *testing.T, fs afero.Fs) (poster.PosterGenerator, string) {
	t.Helper()
	const jobID = "job-migrate"
	dir := filepath.Join("/temp", "posters", jobID)
	require.NoError(t, afero.WriteFile(fs, dir+"/OLD-1-full.jpg", []byte("origin-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dir+"/OLD-1.jpg", []byte("origin-preview"), 0o644))
	return poster.NewScrapePosterGenerator(poster.NewPosterManager(fs, "/temp", nil), "", ""), dir
}

func assertFileBytes(t *testing.T, fs afero.Fs, path, want string) {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err, "%s must exist", path)
	assert.Equal(t, want, string(data), "%s bytes", path)
}

func assertFileGone(t *testing.T, fs afero.Fs, path string) {
	t.Helper()
	_, err := fs.Stat(path)
	assert.True(t, os.IsNotExist(err), "%s must not exist", path)
}

// TestMigratePosterCacheAssets_BlockedDestinationPreservesOrigin pins the
// audit-5 F1 fix for the blocker class: the forward move fails at a
// destination it cannot displace (a NON-EMPTY DIRECTORY surfaces as
// ENOTEMPTY on Remove — same leg position as MoveAssets' destination-replace
// step). The snapshot-based reversal must restore BOTH keys byte-for-byte —
// the origin's surviving full-size file is rewritten from memory, never
// deleted by an absent-source branch — and foreign destination content a
// completed leg replaced is restored from memory, never relocated onto the
// origin key while reporting success.
func TestMigratePosterCacheAssets_BlockedDestinationPreservesOrigin(t *testing.T) {
	base := afero.NewMemMapFs()
	gen, dir := migrateFixture(t, base)
	// Foreign content at the destination key: the full asset cannot be
	// dropped (ENOTEMPTY, as a non-empty directory yields on a real fs);
	// the preview asset is a plain foreign file the completed second leg
	// will replace before the move reports failure.
	require.NoError(t, afero.WriteFile(base, dir+"/NEW-2-full.jpg", []byte("foreign-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/NEW-2.jpg", []byte("foreign-preview"), 0o644))
	fs := &migrateFailFs{Fs: base, removeErr: map[string]error{
		filepath.Join(dir, "NEW-2-full.jpg"): &os.PathError{Op: "remove", Path: filepath.Join(dir, "NEW-2-full.jpg"), Err: os.ErrExist},
	}}
	gen = poster.NewScrapePosterGenerator(poster.NewPosterManager(fs, "/temp", nil), "", "")

	back, err := MigratePosterCacheAssets(gen, "job-migrate", "OLD-1", "NEW-2")
	require.Error(t, err)
	assert.Nil(t, back, "a failed migration returns no compensation closure")
	assert.Contains(t, err.Error(), "migrate poster assets to re-keyed movie NEW-2",
		"the surfaced error names the forward failure")
	assert.Contains(t, err.Error(), "replace destination poster asset NEW-2-full.jpg",
		"the underlying blocked leg is visible too")
	assert.NotContains(t, err.Error(), "partial move reversal failed",
		"the snapshot reversal restored both keys cleanly")

	// Origin SURVIVES byte-for-byte (full-size included) — the pre-fix
	// reverse re-key deleted exactly this file via the absent-source branch
	// when the foreign blocker hid behind the destination name.
	assertFileBytes(t, fs, dir+"/OLD-1-full.jpg", "origin-full")
	assertFileBytes(t, fs, dir+"/OLD-1.jpg", "origin-preview")
	// Foreign blocker content is INTACT at the destination key — never
	// displaced, never relocated onto the origin key.
	assertFileBytes(t, fs, dir+"/NEW-2-full.jpg", "foreign-full")
	// The completed second leg (origin preview renamed onto the foreign
	// preview) is reversed: the foreign preview is restored from the
	// destination snapshot and the origin preview from the origin snapshot.
	assertFileBytes(t, fs, dir+"/NEW-2.jpg", "foreign-preview")
}

// TestMigratePosterCacheAssets_PartialMoveRestoresCompletedLegOnly pins the
// leg-awareness of the snapshot reversal: when only the FIRST leg completed
// (the second leg's rename fails with EPERM), the completed leg is reversed
// exactly, the failed leg's surviving origin file is untouched, and nothing
// is stranded at the destination key — and when NO rename succeeds, the
// origin is trivially intact.
func TestMigratePosterCacheAssets_PartialMoveRestoresCompletedLegOnly(t *testing.T) {
	t.Run("second leg rename EPERM", func(t *testing.T) {
		base := afero.NewMemMapFs()
		_, dir := migrateFixture(t, base)
		fs := &migrateFailFs{Fs: base, renameErr: map[string]error{
			filepath.Join(dir, "OLD-1.jpg"): os.ErrPermission,
		}}
		gen := poster.NewScrapePosterGenerator(poster.NewPosterManager(fs, "/temp", nil), "", "")

		back, err := MigratePosterCacheAssets(gen, "job-migrate", "OLD-1", "NEW-2")
		require.Error(t, err)
		assert.Nil(t, back)
		assert.Contains(t, err.Error(), "migrate poster assets to re-keyed movie NEW-2")
		assert.Contains(t, err.Error(), "move poster asset OLD-1.jpg -> NEW-2.jpg",
			"the surfaced error names the failed forward leg")
		assert.NotContains(t, err.Error(), "partial move reversal failed")

		// Completed full-size leg reversed: destination key is EMPTY…
		assertFileGone(t, fs, dir+"/NEW-2-full.jpg")
		assertFileGone(t, fs, dir+"/NEW-2.jpg")
		// …and the origin's ORIGINALS are intact: the never-renamed preview
		// was untouched, the renamed-away full asset rewritten from memory.
		assertFileBytes(t, fs, dir+"/OLD-1-full.jpg", "origin-full")
		assertFileBytes(t, fs, dir+"/OLD-1.jpg", "origin-preview")
	})

	t.Run("all renames EPERM leave the origin untouched", func(t *testing.T) {
		base := afero.NewMemMapFs()
		_, dir := migrateFixture(t, base)
		fs := &migrateFailFs{Fs: base, renameErr: map[string]error{
			filepath.Join(dir, "OLD-1-full.jpg"): os.ErrPermission,
			filepath.Join(dir, "OLD-1.jpg"):      os.ErrPermission,
		}}
		gen := poster.NewScrapePosterGenerator(poster.NewPosterManager(fs, "/temp", nil), "", "")

		back, err := MigratePosterCacheAssets(gen, "job-migrate", "OLD-1", "NEW-2")
		require.Error(t, err)
		assert.Nil(t, back)
		assert.NotContains(t, err.Error(), "partial move reversal failed")
		assertFileBytes(t, fs, dir+"/OLD-1-full.jpg", "origin-full")
		assertFileBytes(t, fs, dir+"/OLD-1.jpg", "origin-preview")
		assertFileGone(t, fs, dir+"/NEW-2-full.jpg")
		assertFileGone(t, fs, dir+"/NEW-2.jpg")
	})
}

// TestMigratePosterCacheAssets_ReversalFailureSurfacedHonestly pins the
// remaining F1 hazard: when the destination holds foreign content the
// snapshot reversal cannot cleanly displace (restore write EPERM), the
// reversal failure is JOINED onto the surfaced error instead of silently
// swapping or relocating anything — and the foreign content stays put.
func TestMigratePosterCacheAssets_ReversalFailureSurfacedHonestly(t *testing.T) {
	base := afero.NewMemMapFs()
	gen, dir := migrateFixture(t, base)
	require.NoError(t, afero.WriteFile(base, dir+"/NEW-2-full.jpg", []byte("foreign-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/NEW-2.jpg", []byte("foreign-preview"), 0o644))
	fs := &migrateFailFs{
		Fs: base,
		removeErr: map[string]error{
			filepath.Join(dir, "NEW-2-full.jpg"): os.ErrPermission, // forward leg 1 blocked at the destination
		},
		writeErrOn: true,
		writeErr: map[string]error{
			filepath.Join(dir, "NEW-2-full.jpg"): os.ErrPermission, // the snapshot reversal cannot displace it either
		},
	}
	gen = poster.NewScrapePosterGenerator(poster.NewPosterManager(fs, "/temp", nil), "", "")

	back, err := MigratePosterCacheAssets(gen, "job-migrate", "OLD-1", "NEW-2")
	require.Error(t, err)
	assert.Nil(t, back)
	assert.Contains(t, err.Error(), "migrate poster assets to re-keyed movie NEW-2")
	assert.Contains(t, err.Error(), "partial move reversal failed",
		"an un-reversible partial state is surfaced honestly, not swallowed")
	assert.Contains(t, err.Error(), "restore poster asset NEW-2-full.jpg")

	// The foreign blocker was never displaced and never relocated onto the
	// origin key; the origin's full-size asset survives.
	assertFileBytes(t, fs, dir+"/NEW-2-full.jpg", "foreign-full")
	assertFileBytes(t, fs, dir+"/OLD-1-full.jpg", "origin-full")
	assertFileBytes(t, fs, dir+"/OLD-1.jpg", "origin-preview")
}

// TestMigratePosterCacheAssets_SuccessReturnsMoveBack pins the strong
// success contract (audit-6 F-new): the forward migration completes and the
// compensation closure (a later persist/plan failure) replays BOTH pre-move
// snapshots — destination first, then origin — instead of running a
// reversed MovePosterAssets re-key. With no foreign content at either key
// the replay's end state is the one the reverse re-key also produced:
// origin bytes back at the origin key, the destination key empty. (Where
// the mechanisms DIFFER — bystander bytes the completed move destroyed at
// the destination — is pinned by
// TestMigratePosterCacheAssets_SuccessMoveBackRestoresBystanderDestination:
// only the snapshot replay can resurrect them.)
func TestMigratePosterCacheAssets_SuccessReturnsMoveBack(t *testing.T) {
	base := afero.NewMemMapFs()
	gen, dir := migrateFixture(t, base)

	back, err := MigratePosterCacheAssets(gen, "job-migrate", "OLD-1", "NEW-2")
	require.NoError(t, err)
	require.NotNil(t, back)
	assertFileBytes(t, base, dir+"/NEW-2-full.jpg", "origin-full")
	assertFileBytes(t, base, dir+"/NEW-2.jpg", "origin-preview")
	assertFileGone(t, base, dir+"/OLD-1-full.jpg")

	require.NoError(t, back(), "compensation moves the completed migration back")
	assertFileBytes(t, base, dir+"/OLD-1-full.jpg", "origin-full")
	assertFileBytes(t, base, dir+"/OLD-1.jpg", "origin-preview")
	assertFileGone(t, base, dir+"/NEW-2-full.jpg")
	assertFileGone(t, base, dir+"/NEW-2.jpg")
}

// TestMigratePosterCacheAssets_SuccessMoveBackRestoresBystanderDestination
// pins the audit-6 F-new fix: the destination key carried a BYSTANDER's
// bytes before the re-key, and the fully completed forward move
// destructively replaced them (MoveAssets removes the destination before
// renaming over it). The compensation closure — built from the pre-move
// snapshots because the real generator exposes the snapshot capability —
// must resurrect the bystander's exact bytes at the destination key AND
// restore the origin bytes at the origin key. Under the pre-fix reverse
// re-key the bystander bytes were lost for good: the reverse move lands the
// origin files back on the origin key but can only DELETE the relocated
// files at the destination, never recreate content it never captured.
func TestMigratePosterCacheAssets_SuccessMoveBackRestoresBystanderDestination(t *testing.T) {
	base := afero.NewMemMapFs()
	gen, dir := migrateFixture(t, base)
	// A bystander movie owns the destination key before the re-key.
	require.NoError(t, afero.WriteFile(base, dir+"/NEW-2-full.jpg", []byte("bystander-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/NEW-2.jpg", []byte("bystander-preview"), 0o644))

	back, err := MigratePosterCacheAssets(gen, "job-migrate", "OLD-1", "NEW-2")
	require.NoError(t, err)
	require.NotNil(t, back)
	// The completed move adopted the bystander's key: the origin bytes are
	// filed under it now, and the bystander bytes were destroyed in place.
	assertFileBytes(t, base, dir+"/NEW-2-full.jpg", "origin-full")
	assertFileBytes(t, base, dir+"/NEW-2.jpg", "origin-preview")
	assertFileGone(t, base, dir+"/OLD-1-full.jpg")
	assertFileGone(t, base, dir+"/OLD-1.jpg")

	require.NoError(t, back(), "compensation replays both pre-move snapshots")
	// The bystander's bytes are resurrected at the destination key…
	assertFileBytes(t, base, dir+"/NEW-2-full.jpg", "bystander-full")
	assertFileBytes(t, base, dir+"/NEW-2.jpg", "bystander-preview")
	// …and the origin's bytes are back at the origin key.
	assertFileBytes(t, base, dir+"/OLD-1-full.jpg", "origin-full")
	assertFileBytes(t, base, dir+"/OLD-1.jpg", "origin-preview")
}

// TestMigratePosterCacheAssets_NoSnapshotCompensationFallsBackToReverseRekey
// pins the no-snapshot arm of the success contract: a mover WITHOUT the
// snapshot capability cannot replay a state it never captured, so its
// compensation degrades to the reverse MovePosterAssets re-key — the one
// direction where MoveAssets inverts a FULLY completed move 1:1 (the origin
// key is empty at every leg, so the absent-source branches cannot delete
// anything real).
func TestMigratePosterCacheAssets_NoSnapshotCompensationFallsBackToReverseRekey(t *testing.T) {
	gen := &moverOnlyStubGen{}
	back, err := MigratePosterCacheAssets(gen, "job-migrate", "OLD-1", "NEW-2")
	require.NoError(t, err)
	require.NotNil(t, back, "a mover-only generator still gets the reverse re-key compensation")

	require.NoError(t, back())
	assert.Equal(t, [][2]string{{"OLD-1", "NEW-2"}, {"NEW-2", "OLD-1"}}, gen.calls,
		"forward move, then the reverse re-key — the only safe compensation without snapshots")
}

// TestMigratePosterCacheAssets_NoMoverDegrades pins the state-only degrade:
// a generator without the move capability does not even snapshot.
func TestMigratePosterCacheAssets_NoMoverDegrades(t *testing.T) {
	back, err := MigratePosterCacheAssets(&recordingPosterGen{}, "job-migrate", "OLD-1", "NEW-2")
	require.NoError(t, err)
	assert.Nil(t, back)
}
