package poster

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failFirstWriteOnSuffix wraps a filesystem so Create of a matching file
// succeeds but its FIRST Write fails — the crash-mid-write the C6 staging
// guards against. Exactly one file is poisoned (armed=false after the hit).
type failFirstWriteOnSuffix struct {
	afero.Fs
	suffix string
	armed  bool
}

func (f *failFirstWriteOnSuffix) Create(name string) (afero.File, error) {
	file, err := f.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	if f.armed && strings.HasSuffix(filepath.ToSlash(name), f.suffix) {
		f.armed = false
		return &failOnceFile{File: file}, nil
	}
	return file, nil
}

type failOnceFile struct {
	afero.File
	fired bool
}

func (w *failOnceFile) Write(p []byte) (int, error) {
	if !w.fired {
		w.fired = true
		return 0, errors.New("injected crash-mid-write failure")
	}
	return w.File.Write(p)
}

// TestCropWithBounds_CrashMidWriteLeavesLivePreviewIntact pins C6: the crop
// must be staged through {posterID}.jpg.tmp and installed by rename, so a
// crash (or encode/write failure) midway through the write never leaves a
// truncated LIVE preview behind.
func TestCropWithBounds_CrashMidWriteLeavesLivePreviewIntact(t *testing.T) {
	mem := afero.NewMemMapFs()
	fs := &failFirstWriteOnSuffix{Fs: mem, suffix: "/CRW-001.jpg.tmp", armed: true}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", nil).WithSSRFCheck(func(string) error { return nil })

	jobID := "job-a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	posterID := "CRW-001"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	sourcePath := filepath.Join(dir, posterID+"-full.jpg")
	previewPath := filepath.Join(dir, posterID+".jpg")
	require.NoError(t, createTestJPEG(fs, sourcePath, 800, 500))
	// Pre-existing live preview the crashed crop must not clobber.
	oldPreview := []byte("previous-preview-bytes")
	require.NoError(t, afero.WriteFile(fs, previewPath, oldPreview, 0o644))

	_, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.Error(t, err, "the injected write failure must surface")

	got, readErr := afero.ReadFile(mem, previewPath)
	require.NoError(t, readErr)
	assert.Equal(t, oldPreview, got, "crash mid-write must leave the live preview intact (C6)")
	_, statErr := mem.Stat(previewPath + ".tmp")
	assert.True(t, errors.Is(statErr, afero.ErrFileNotFound), "the staging temp must be removed on the failure leg: %v", statErr)
}

// TestCropWithBounds_RenamesStagingIntoPlace pins the happy path: the staged
// temp is renamed onto the preview and does not linger afterwards.
func TestCropWithBounds_RenamesStagingIntoPlace(t *testing.T) {
	mem := afero.NewMemMapFs()
	pm := NewPosterManager(mem, "/tmp/javinizer-test", nil).WithSSRFCheck(func(string) error { return nil })

	jobID := "job-a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	posterID := "CRW-002"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	require.NoError(t, createTestJPEG(mem, filepath.Join(dir, posterID+"-full.jpg"), 800, 500))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, posterID+".jpg"), []byte("old"), 0o644))

	res, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.NoError(t, err)
	require.NotNil(t, res)

	_, statErr := mem.Stat(filepath.Join(dir, posterID+".jpg.tmp"))
	assert.Error(t, statErr, "no staging temp may linger after a successful crop")
	got, readErr := afero.ReadFile(mem, filepath.Join(dir, posterID+".jpg"))
	require.NoError(t, readErr)
	assert.NotEqual(t, []byte("old"), got, "the preview must be replaced by the crop")
}

// TestCropWithBounds_SweepsOwnOrphanedStagingTemp pins the entry-time sweep
// of THIS posterID's own staging path: a crash that stranded a stale
// {posterID}.jpg.tmp is cleared before the next crop of the same poster.
// Only the own path may be swept (P1-1): per-movie locks let different
// movies of the same job crop concurrently, so a dir-wide sweep could
// delete a sibling crop's in-flight staging — foreign *.jpg.tmp files are
// left alone (see TestCropWithBounds_PreservesConcurrentSiblingStaging).
func TestCropWithBounds_SweepsOwnOrphanedStagingTemp(t *testing.T) {
	mem := afero.NewMemMapFs()
	pm := NewPosterManager(mem, "/tmp/javinizer-test", nil).WithSSRFCheck(func(string) error { return nil })

	jobID := "job-a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	posterID := "CRW-003"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	require.NoError(t, createTestJPEG(mem, filepath.Join(dir, posterID+"-full.jpg"), 800, 500))
	orphan := filepath.Join(dir, posterID+".jpg.tmp")
	require.NoError(t, afero.WriteFile(mem, orphan, []byte("orphaned staging"), 0o644))

	_, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.NoError(t, err)

	_, statErr := mem.Stat(orphan)
	assert.Error(t, statErr, "the entry sweep must remove this poster's own orphaned staging temp")
}

// TestCropWithBounds_PreservesConcurrentSiblingStaging pins Codex P1-1: the
// staging sweep may only remove THIS posterID's own {posterID}.jpg.tmp.
// Per-movie locks let two movies of the SAME job crop concurrently and the
// staging files share the job's cache directory, so a dir-wide *.jpg.tmp
// sweep deleted the sibling crop's staged output between its write and its
// rename — make sure a foreign staging file survives an unrelated crop.
// The own-path stale sweep still runs (a leftover own staging must be
// cleared before re-cropping).
func TestCropWithBounds_PreservesConcurrentSiblingStaging(t *testing.T) {
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp/javinizer-test", nil).WithSSRFCheck(func(string) error { return nil })

	jobID := "job-a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	posterID := "OWN-001"
	siblingID := "SIB-001"
	require.NoError(t, createTestJPEG(fs, filepath.Join(dir, posterID+"-full.jpg"), 800, 500))
	require.NoError(t, createTestJPEG(fs, filepath.Join(dir, siblingID+"-full.jpg"), 800, 500))

	// The sibling's in-flight staged output (its crop wrote it, rename pending).
	siblingStaging := filepath.Join(dir, siblingID+".jpg.tmp")
	siblingBytes := []byte("sibling-crop-staged-output")
	require.NoError(t, afero.WriteFile(fs, siblingStaging, siblingBytes, 0o644))
	// A stale leftover of OUR staging path must still be swept.
	ownStale := filepath.Join(dir, posterID+".jpg.tmp")
	require.NoError(t, afero.WriteFile(fs, ownStale, []byte("stale-own"), 0o644))

	_, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.NoError(t, err)

	got, readErr := afero.ReadFile(fs, siblingStaging)
	require.NoError(t, readErr, "a crop of %s must not delete %s's in-flight staging file", posterID, siblingID)
	assert.Equal(t, siblingBytes, got)
	_, statErr := fs.Stat(ownStale)
	assert.True(t, errors.Is(statErr, afero.ErrFileNotFound),
		"the crop's own stale staging temp must still be swept up front: %v", statErr)
}

// windowsRenameFs emulates Windows rename semantics for the platform-safe
// replace tests: os.Rename on Windows (the real OsFs the "Unit Tests
// (Windows)" CI job exercises) refuses to overwrite an EXISTING destination,
// while afero's MemMapFs happily replaces — masking this failure class.
type windowsRenameFs struct{ afero.Fs }

func (f *windowsRenameFs) Rename(oldPath, newPath string) error {
	if _, err := f.Fs.Stat(newPath); err == nil {
		return errors.New("windows rename semantics: destination exists")
	}
	return f.Fs.Rename(oldPath, newPath)
}

// TestCropWithBounds_RecropOverExistingPreviewWindowsSafe pins round-12 P1-A:
// a normal recrop installs the staged crop over the EXISTING preview. A plain
// staged rename failed every recrop on Windows (destination exists); the
// install must stage the old preview aside (dest→.bak, the downloader's
// protocol in internal/downloader/media.go) and clean the backup up after.
func TestCropWithBounds_RecropOverExistingPreviewWindowsSafe(t *testing.T) {
	mem := afero.NewMemMapFs()
	fs := &windowsRenameFs{Fs: mem}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", nil).WithSSRFCheck(func(string) error { return nil })

	jobID := "job-a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	posterID := "REC-001"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	require.NoError(t, createTestJPEG(fs, filepath.Join(dir, posterID+"-full.jpg"), 800, 500))

	// First crop establishes the pre-existing live preview (the destination
	// every subsequent recrop overwrites).
	_, err := pm.CropWithBounds(context.Background(), jobID, posterID, 0, 0, 200, 300, 0)
	require.NoError(t, err)
	first, err := afero.ReadFile(mem, filepath.Join(dir, posterID+".jpg"))
	require.NoError(t, err)

	// The recrop must succeed even though the destination exists.
	res, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.NoError(t, err, "recrop over an existing preview must work under Windows rename semantics")
	require.NotNil(t, res)

	second, err := afero.ReadFile(mem, filepath.Join(dir, posterID+".jpg"))
	require.NoError(t, err)
	assert.NotEqual(t, first, second, "the recrop must install the new preview")
	_, statErr := mem.Stat(filepath.Join(dir, posterID+".jpg.bak"))
	assert.Error(t, statErr, "no backup may linger after a successful recrop")
	_, statErr = mem.Stat(filepath.Join(dir, posterID+".jpg.tmp"))
	assert.Error(t, statErr, "no staging temp may linger after a successful recrop")
}

// failInstallRenameFs refuses ONLY the install rename (staging → preview) so
// the rollback leg of the backup protocol can be exercised.
type failInstallRenameFs struct{ afero.Fs }

func (f *failInstallRenameFs) Rename(oldPath, newPath string) error {
	if strings.HasSuffix(filepath.ToSlash(oldPath), ".jpg.tmp") {
		return errors.New("forced install rename failure")
	}
	return f.Fs.Rename(oldPath, newPath)
}

// TestCropWithBounds_InstallFailureRollsBackExistingPreview pins the rollback
// half of the backup protocol: a failed install after the old preview was
// staged aside must restore it, never leave the slot empty.
func TestCropWithBounds_InstallFailureRollsBackExistingPreview(t *testing.T) {
	mem := afero.NewMemMapFs()
	fs := &failInstallRenameFs{Fs: mem}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", nil).WithSSRFCheck(func(string) error { return nil })

	jobID := "job-a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	posterID := "REC-002"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	require.NoError(t, createTestJPEG(fs, filepath.Join(dir, posterID+"-full.jpg"), 800, 500))
	oldPreview := []byte("existing preview bytes")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, posterID+".jpg"), oldPreview, 0o644))

	_, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.Error(t, err, "the forced install failure must surface")
	assert.Contains(t, err.Error(), "failed to install cropped poster preview")

	got, readErr := afero.ReadFile(mem, filepath.Join(dir, posterID+".jpg"))
	require.NoError(t, readErr, "a failed install must roll the old preview back, not leave the slot empty")
	assert.Equal(t, oldPreview, got)
	_, statErr := mem.Stat(filepath.Join(dir, posterID+".jpg.tmp"))
	assert.Error(t, statErr, "the staging temp must be cleaned on the failure leg")
}

// TestCropWithBounds_RecoversCrashedReplaceBackup pins crash recovery: a
// previous recrop that died after staging the old preview aside left the
// slot empty with the only copy in .bak — the next crop must restore-or-
// replace without losing it, and a FAILED next crop must find the old
// preview back in place.
func TestCropWithBounds_RecoversCrashedReplaceBackup(t *testing.T) {
	mem := afero.NewMemMapFs()
	fs := &windowsRenameFs{Fs: mem}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", nil).WithSSRFCheck(func(string) error { return nil })

	jobID := "job-a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	posterID := "REC-003"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	require.NoError(t, createTestJPEG(fs, filepath.Join(dir, posterID+"-full.jpg"), 800, 500))
	crashBackup := []byte("only surviving copy of the old preview")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, posterID+".jpg.bak"), crashBackup, 0o644))

	// Healthy path: the stranded backup is consumed and the crop installs.
	_, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.NoError(t, err)
	got, readErr := afero.ReadFile(mem, filepath.Join(dir, posterID+".jpg"))
	require.NoError(t, readErr)
	assert.NotEqual(t, crashBackup, got, "the new crop must replace the recovered preview")
	_, statErr := mem.Stat(filepath.Join(dir, posterID+".jpg.bak"))
	assert.Error(t, statErr, "the consumed backup must not linger")
}

// failBackupRecoveryRenameFs refuses ONLY the crash-recovery rename
// (.bak → preview) so the fail-closed recovery leg can be exercised.
type failBackupRecoveryRenameFs struct{ afero.Fs }

func (f *failBackupRecoveryRenameFs) Rename(oldPath, newPath string) error {
	if strings.HasSuffix(filepath.ToSlash(oldPath), ".jpg.bak") {
		return errors.New("forced backup recovery failure")
	}
	return f.Fs.Rename(oldPath, newPath)
}

// TestCropWithBounds_FailedBackupRecoveryFailsClosed pins Codex P1-3: a
// crashed previous replace left the preview slot empty with the only
// valid copy of the old preview in .bak. When the recovery rename itself
// fails, CropWithBounds must SURFACE the error and fail closed —
// swallowing it (the previous `_ = pm.fs.Rename(...)`) let the new crop
// install over an unrecovered slot while the referenced preview stayed
// missing and the only valid copy remained stranded in .bak.
func TestCropWithBounds_FailedBackupRecoveryFailsClosed(t *testing.T) {
	mem := afero.NewMemMapFs()
	fs := &failBackupRecoveryRenameFs{Fs: mem}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", nil).WithSSRFCheck(func(string) error { return nil })

	jobID := "job-a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	posterID := "REC-004"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	require.NoError(t, createTestJPEG(fs, filepath.Join(dir, posterID+"-full.jpg"), 800, 500))
	onlyCopy := []byte("only surviving copy of the old preview")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, posterID+".jpg.bak"), onlyCopy, 0o644))

	_, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.Error(t, err, "a failed .bak recovery must surface, not be hidden by installing the new crop")
	assert.Contains(t, err.Error(), "failed to recover interrupted poster preview backup")

	got, readErr := afero.ReadFile(mem, filepath.Join(dir, posterID+".jpg.bak"))
	require.NoError(t, readErr, "the only surviving copy must remain in .bak, not be destroyed")
	assert.Equal(t, onlyCopy, got)
	_, statErr := mem.Stat(filepath.Join(dir, posterID+".jpg"))
	assert.Error(t, statErr, "the referenced preview slot must stay empty when recovery failed")
}
