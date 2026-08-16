package poster

// POSTER-WRITE-HARDENING P2 red suite (spec apply-writeback-coherence:
// "Poster preview installs are staged and recoverable").

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSpyFS records every create/truncating-open so a test can prove the
// final preview path is installed by RENAME ONLY — readers mid-replace can
// then never observe partially-written bytes (byte-atomicity proxy).
type writeSpyFS struct {
	afero.Fs
	mu     sync.Mutex
	writes map[string]int
}

func (s *writeSpyFS) note(n string) {
	s.mu.Lock()
	s.writes[filepath.ToSlash(n)]++
	s.mu.Unlock()
}

func (s *writeSpyFS) Create(n string) (afero.File, error) {
	s.note(n)
	return s.Fs.Create(n)
}

func (s *writeSpyFS) countExact(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes[filepath.ToSlash(path)]
}

// Failed install ⇒ the previous preview survives byte-for-byte, the error
// surfaces, and no staging or backup residue is left behind.
func TestPosterManager_CropWithBounds_FailedInstallKeepsPreviousPreview(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-1-full.jpg", jpegBytes(200, 300), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-1.jpg", []byte("old-preview-bytes"), 0o644))

	// Wedge ONLY the staged→final install rename; the aside/restore renames
	// (from a .bak sibling) stay live so the restore path can fire.
	base2 := &renameFailWhereFS{Fs: base, fail: func(o, n string) bool {
		return strings.Contains(o, ".tmp") && strings.HasSuffix(n, "/ST-1.jpg")
	}}
	pm := NewPosterManager(base2, "/tmp/p2", nil, 0)

	_, err := pm.CropWithBounds(context.Background(), "job1", "ST-1", 0, 0, 100, 150, 0)
	require.Error(t, err, "install failure must surface")

	got, rerr := afero.ReadFile(base, dir+"/ST-1.jpg")
	require.NoError(t, rerr)
	assert.Equal(t, "old-preview-bytes", string(got), "previous preview intact after failed install")

	entries, derr := afero.ReadDir(base, dir)
	require.NoError(t, derr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no staged residue: %s", e.Name())
		assert.NotContains(t, e.Name(), ".bak", "no backup residue: %s", e.Name())
	}
}

// Byte-atomicity contract: a successful crop installs the new preview via a
// same-directory staged temp + rename — the final preview path is NEVER
// opened for writing by the crop itself, so a concurrent reader sees either
// the old or the new bytes, never a partial write.
func TestPosterManager_CropWithBounds_InstallWritesFinalPathOnlyViaRename(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-2-full.jpg", jpegBytes(200, 300), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-2.jpg", []byte("old-preview-bytes"), 0o644))

	spy := &writeSpyFS{Fs: base, writes: map[string]int{}}
	pm := NewPosterManager(spy, "/tmp/p2", nil, 0)

	res, err := pm.CropWithBounds(context.Background(), "job1", "ST-2", 0, 0, 100, 150, 0)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 0, spy.countExact(dir+"/ST-2.jpg"),
		"final preview installed by rename only (staged temp carried the bytes)")

	got, rerr := afero.ReadFile(base, dir+"/ST-2.jpg")
	require.NoError(t, rerr)
	assert.NotEqual(t, "old-preview-bytes", string(got), "new preview installed")
}

// Snapshot/restore compensation: SnapshotAssets captures the (jobID,
// posterID) asset pair; RestoreAssets puts the captured bytes back — and
// deletes assets CREATED after the snapshot (compensation is complete: it
// doesn't just overwrite, it also removes what didn't exist before).
func TestPosterManager_SnapshotRestore_RoundTrip(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/SN-1-full.jpg", jpegBytes(200, 300), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/SN-1.jpg", []byte("crop-v1"), 0o644))
	pm := NewPosterManager(base, "/tmp/p2", nil, 0)

	snap, err := pm.SnapshotAssets("job1", "SN-1")
	require.NoError(t, err)
	require.NotNil(t, snap)

	// Mutation + removal after the snapshot.
	require.NoError(t, base.Remove(dir+"/SN-1.jpg"))
	require.NoError(t, afero.WriteFile(base, dir+"/SN-1-full.jpg", []byte("mutated"), 0o644))

	require.NoError(t, pm.RestoreAssets(snap))
	got, rerr := afero.ReadFile(base, dir+"/SN-1.jpg")
	require.NoError(t, rerr, "removed crop leg restored")
	assert.Equal(t, "crop-v1", string(got))
	full, ferr := afero.ReadFile(base, dir+"/SN-1-full.jpg")
	require.NoError(t, ferr)
	assert.Equal(t, jpegBytes(200, 300), full, "mutated full leg restored")
}

// Created-asset removal: assets that did not exist at snapshot time are
// deleted on restore — compensation leaves the dir exactly as captured.
func TestPosterManager_SnapshotRestore_RemovesCreatedAssets(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/SN-2-full.jpg", jpegBytes(200, 300), 0o644))
	pm := NewPosterManager(base, "/tmp/p2", nil, 0)

	snap, err := pm.SnapshotAssets("job1", "SN-2")
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(base, dir+"/SN-2.jpg", jpegBytes(100, 150), 0o644))

	require.NoError(t, pm.RestoreAssets(snap))
	_, err2 := base.Stat(dir + "/SN-2.jpg")
	assert.Error(t, err2, "asset created after snapshot is removed on restore")
}

// --- wedge FS wrappers for the machinery arms ---

type removeFailWhereFS struct {
	afero.Fs
	fail func(name string) bool
}

func (f removeFailWhereFS) Remove(name string) error {
	if f.fail(filepath.ToSlash(name)) {
		return errWedge
	}
	return f.Fs.Remove(name)
}

// openFileFailWhereFS wedges write-creates (afero.WriteFile routes through
// OpenFile with O_CREATE, not Create) — the staged-write fault injector.
type openFileFailWhereFS struct {
	afero.Fs
	fail func(name string, flag int) bool
}

func (f openFileFailWhereFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.fail(filepath.ToSlash(name), flag) {
		return nil, errWedge
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type openFailSuffixFS struct {
	afero.Fs
	suffix string
}

func (f openFailSuffixFS) Open(name string) (afero.File, error) {
	if strings.HasSuffix(filepath.ToSlash(name), f.suffix) {
		return nil, errWedge
	}
	return f.Fs.Open(name)
}

var errWedge = errWedgeType{}

type errWedgeType struct{}

func (errWedgeType) Error() string { return "wedged" }

// The backup-aside step (a COPY under P3's never-absent contract) wedging:
// the crop aborts BEFORE any byte damage, the previous preview is intact,
// and the staged temp is cleaned by the caller.
func TestPosterManager_CropWithBounds_AsideFailureKeepsEverything(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-3-full.jpg", jpegBytes(200, 300), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-3.jpg", []byte("old-preview"), 0o644))
	wedged := openFileFailWhereFS{Fs: base, fail: func(n string, flag int) bool {
		return strings.Contains(n, ".bak") && flag&os.O_CREATE != 0
	}}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)

	_, err := pm.CropWithBounds(context.Background(), "job1", "ST-3", 0, 0, 100, 150, 0)
	require.ErrorContains(t, err, "back up previous preview")
	got, rerr := afero.ReadFile(base, dir+"/ST-3.jpg")
	require.NoError(t, rerr)
	assert.Equal(t, "old-preview", string(got))
	entries, derr := afero.ReadDir(base, dir)
	require.NoError(t, derr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no staged residue: %s", e.Name())
		assert.NotContains(t, e.Name(), ".bak", "no backup residue: %s", e.Name())
	}
}

// Total-loss leg: the staged install itself wedges. With the never-absent
// canonical contract there is NO opportunistic restore arm — the old preview
// sits untouched throughout, and the (copied) backup is swept.
func TestPosterManager_CropWithBounds_InstallFailureKeepsOld(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-4-full.jpg", jpegBytes(200, 300), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-4.jpg", []byte("old-preview"), 0o644))
	wedged := renameFailWhereFS{Fs: base, fail: func(o, n string) bool {
		return strings.Contains(o, ".tmp") && strings.HasSuffix(n, "/ST-4.jpg")
	}}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)

	_, err := pm.CropWithBounds(context.Background(), "job1", "ST-4", 0, 0, 100, 150, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install staged preview")
	// Old preview untouched at canonical; any backup copy swept; no staged
	// residue. Nothing was ever absent (the canonical entry stayed put).
	got, rerr := afero.ReadFile(base, dir+"/ST-4.jpg")
	require.NoError(t, rerr)
	assert.Equal(t, "old-preview", string(got))
	entries, derr := afero.ReadDir(base, dir)
	require.NoError(t, derr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no staged residue: %s", e.Name())
		assert.NotContains(t, e.Name(), ".bak", "backup swept on failure: %s", e.Name())
	}
}

// A wedged backup sweep after a successful install: the crop succeeds, the
// warn fires (leg covers the sweep arm), and the leftover backup stays for
// inspection (never blocking the success path).
func TestPosterManager_CropWithBounds_BackupSweepWarnKeepsSuccess(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-5-full.jpg", jpegBytes(200, 300), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-5.jpg", []byte("old-preview"), 0o644))
	wedged := removeFailWhereFS{Fs: base, fail: func(n string) bool { return strings.HasSuffix(n, ".bak") }}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)

	res, err := pm.CropWithBounds(context.Background(), "job1", "ST-5", 0, 0, 100, 150, 0)
	require.NoError(t, err, "sweep wedge is warn-only")
	require.NotNil(t, res)
	got, rerr := afero.ReadFile(base, dir+"/ST-5.jpg")
	require.NoError(t, rerr)
	assert.NotEqual(t, "old-preview", string(got), "new preview installed")
}

// Snapshot read faults (non-NotExist) surface instead of recording absence.
func TestPosterManager_SnapshotAssets_ReadFaultSurfaces(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/SN-9-full.jpg", jpegBytes(200, 300), 0o644))
	pm := NewPosterManager(openFailSuffixFS{Fs: base, suffix: "/SN-9-full.jpg"}, "/tmp/p2", nil, 0)
	_, err := pm.SnapshotAssets("job1", "SN-9")
	require.ErrorContains(t, err, "snapshot assets")
}

// Nil snapshot is a no-op; the crop-leg read fault also surfaces.
func TestPosterManager_SnapshotRestore_EdgeArms(t *testing.T) {
	base := afero.NewMemMapFs()
	pm := NewPosterManager(base, "/tmp/p2", nil, 0)
	require.NoError(t, pm.RestoreAssets(nil))

	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/SN-8.jpg", []byte("crop"), 0o644))
	pm2 := NewPosterManager(openFailSuffixFS{Fs: base, suffix: "/SN-8.jpg"}, "/tmp/p2", nil, 0)
	_, err := pm2.SnapshotAssets("job1", "SN-8")
	require.ErrorContains(t, err, "snapshot assets")
}

// restoreLeg failure arms: removed-leg remove-wedge and staged-write wedge
// both surface errors instead of half-restoring.
func TestPosterManager_RestoreAssets_LegFailureArms(t *testing.T) {
	// remove wedge on a created leg
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/SN-3-full.jpg", jpegBytes(200, 300), 0o644))
	pm := NewPosterManager(base, "/tmp/p2", nil, 0)
	snap, err := pm.SnapshotAssets("job1", "SN-3")
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(base, dir+"/SN-3.jpg", jpegBytes(100, 150), 0o644))

	wedged := removeFailWhereFS{Fs: base, fail: func(n string) bool { return strings.HasSuffix(n, "/SN-3.jpg") }}
	pmWedged := NewPosterManager(wedged, "/tmp/p2", nil, 0)
	require.ErrorContains(t, pmWedged.RestoreAssets(snap), "remove created leg")

	// staged-write wedge when restoring an existing leg
	base2 := afero.NewMemMapFs()
	require.NoError(t, base2.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base2, dir+"/SN-4.jpg", []byte("v1"), 0o644))
	pm2 := NewPosterManager(base2, "/tmp/p2", nil, 0)
	snap2, err := pm2.SnapshotAssets("job1", "SN-4")
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(base2, dir+"/SN-4.jpg", []byte("v2"), 0o644))

	stageWedged := openFileFailWhereFS{Fs: base2, fail: func(n string, flag int) bool {
		return strings.HasSuffix(n, ".tmp") && flag&os.O_CREATE != 0
	}}
	pm3 := NewPosterManager(stageWedged, "/tmp/p2", nil, 0)
	require.ErrorContains(t, pm3.RestoreAssets(snap2), "stage leg")
	got, rerr := afero.ReadFile(base2, dir+"/SN-4.jpg")
	require.NoError(t, rerr)
	assert.Equal(t, "v2", string(got), "failed restore staged-nothing ⇒ target untouched")

	// install wedge when restoring: error surfaces and no residue remains
	installWedged := renameFailWhereFS{Fs: base2, fail: func(o, n string) bool { return strings.Contains(o, ".tmp") }}
	pm4 := NewPosterManager(installWedged, "/tmp/p2", nil, 0)
	require.ErrorContains(t, pm4.RestoreAssets(snap2), "install leg")
	entries, derr := afero.ReadDir(base2, dir)
	require.NoError(t, derr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no staged residue: %s", e.Name())
	}
}

// Validation arms: a hostile jobID/posterID fails before any fs access.
func TestPosterManager_SnapshotRestore_ValidationArms(t *testing.T) {
	pm := NewPosterManager(afero.NewMemMapFs(), "/tmp/p2", nil, 0)
	if _, err := pm.SnapshotAssets("../escape", "V-1"); err == nil {
		t.Fatal("invalid jobID must fail")
	}
	if _, err := pm.SnapshotAssets("job1", "../V-1"); err == nil {
		t.Fatal("invalid posterID must fail")
	}
}

// The full-leg restore erroring aborts before the crop leg is touched.
func TestPosterManager_RestoreAssets_FullLegFailureAbortsCropLeg(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/SN-7-full.jpg", jpegBytes(200, 300), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/SN-7.jpg", []byte("crop-v1"), 0o644))
	pm := NewPosterManager(base, "/tmp/p2", nil, 0)
	snap, err := pm.SnapshotAssets("job1", "SN-7")
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(base, dir+"/SN-7.jpg", []byte("crop-v2"), 0o644))

	// Wedge staged writes ONLY for the full leg's staging name.
	wedged := openFileFailWhereFS{Fs: base, fail: func(n string, flag int) bool {
		return strings.Contains(n, "SN-7-full.jpg.") && strings.HasSuffix(n, ".tmp") && flag&os.O_CREATE != 0
	}}
	pmWedged := NewPosterManager(wedged, "/tmp/p2", nil, 0)
	err2 := pmWedged.RestoreAssets(snap)
	require.ErrorContains(t, err2, "stage leg")
	got, rerr := afero.ReadFile(base, dir+"/SN-7.jpg")
	require.NoError(t, rerr)
	assert.Equal(t, "crop-v2", string(got), "crop leg untouched after full-leg failure")
}

// A wedged staged-file cleanup after a failed crop is warn-only.
func TestPosterManager_CropWithBounds_StagedCleanupWarnOnFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-6-full.jpg", jpegBytes(200, 300), 0o644))
	wedged := removeFailWhereFS{Fs: base, fail: func(n string) bool { return strings.HasSuffix(n, ".tmp") }}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)

	// Out-of-range bounds: crop fails after the staged name is allocated.
	_, err := pm.CropWithBounds(context.Background(), "job1", "ST-6", 0, 0, 99999, 99999, 0)
	require.ErrorContains(t, err, "crop failed")
}

// codex P2 (PR211, third finding): an undecidable destination Stat (neither
// success nor clean absence) fails closed — the staged bytes never install
// over a possibly-existing preview we couldn't back up.
func TestInstallStagedPreview_StatWedgeFailsClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/FC-1.jpg", []byte("old-preview"), 0o644))
	wedged := statFailExactFS{Fs: base, path: dir + "/FC-1.jpg"}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)

	staged := dir + "/FC-1.jpg.staged.tmp"
	require.NoError(t, afero.WriteFile(base, staged, []byte("new-preview"), 0o644))
	err := pm.installStagedPreview(dir+"/FC-1.jpg", staged)
	require.ErrorContains(t, err, "failed to probe preview")

	got, rerr := afero.ReadFile(base, dir+"/FC-1.jpg")
	require.NoError(t, rerr)
	assert.Equal(t, "old-preview", string(got), "prior preview intact under the wedge")
	stagedBytes, serr := afero.ReadFile(base, staged)
	require.NoError(t, serr)
	assert.Equal(t, "new-preview", string(stagedBytes), "staged bytes preserved for a retry")
	entries, _ := afero.ReadDir(base, dir)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".bak", "no partial backup from the fail-closed arm")
	}
}

// codex P2 (PR211, round-3 finding): a staged install NEVER lets the
// canonical preview disappear — even if removing the final path is wedged
// mid-op (modeling a reader holding it), the swap still lands atomically.
func TestInstallStagedPreview_NeverRemovesCanonical(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/NG-1.jpg", []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/NG-1.jpg.staged.tmp", []byte("new"), 0o644))
	wedgeRemoveFinal := removeFailWhereFS{Fs: base, fail: func(n string) bool {
		return strings.HasSuffix(n, "/NG-1.jpg")
	}}
	pm := NewPosterManager(wedgeRemoveFinal, "/tmp/p2", nil, 0)

	err := pm.installStagedPreview(dir+"/NG-1.jpg", dir+"/NG-1.jpg.staged.tmp")
	require.NoError(t, err, "no Remove(final) step exists at all — swap is rename-atomic")
	got, rerr := afero.ReadFile(base, dir+"/NG-1.jpg")
	require.NoError(t, rerr)
	assert.Equal(t, "new", string(got))
}

// Failed-install backup sweep is warn-only when the sweep itself wedges.
func TestInstallStagedPreview_FailedInstallBackupSweepWarnOnly(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/NG-2.jpg", []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/NG-2.jpg.staged.tmp", []byte("new"), 0o644))
	combo := removeFailWhereFS{Fs: renameFailWhereFS{Fs: base, fail: func(o, n string) bool {
		return strings.Contains(o, ".tmp") && strings.HasSuffix(n, "/NG-2.jpg")
	}}, fail: func(n string) bool { return strings.HasSuffix(n, ".bak") }}
	pm := NewPosterManager(combo, "/tmp/p2", nil, 0)

	err := pm.installStagedPreview(dir+"/NG-2.jpg", dir+"/NG-2.jpg.staged.tmp")
	require.ErrorContains(t, err, "failed to install staged preview")
	got, rerr := afero.ReadFile(base, dir+"/NG-2.jpg")
	require.NoError(t, rerr)
	assert.Equal(t, "old", string(got))
}
