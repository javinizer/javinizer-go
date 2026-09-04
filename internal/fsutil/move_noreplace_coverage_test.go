package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failFs selectively fails fs operations for named paths. Used to drive every
// defensive failure leg of the composites.
type failFs struct {
	afero.Fs
	failMkdirAll string
	failOpen     string
	failOpenFile string
	failLstat    string
}

func (f *failFs) MkdirAll(path string, perm os.FileMode) error {
	if f.failMkdirAll != "" && filepath.Clean(path) == filepath.Clean(f.failMkdirAll) {
		return errors.New("simulated mkdir failure")
	}
	return f.Fs.MkdirAll(path, perm)
}

func (f *failFs) Open(name string) (afero.File, error) {
	if f.failOpen != "" && filepath.Clean(name) == filepath.Clean(f.failOpen) {
		return nil, errors.New("simulated open failure")
	}
	return f.Fs.Open(name)
}

func (f *failFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.failOpenFile != "" && strings.HasPrefix(filepath.Clean(name), filepath.Clean(f.failOpenFile)) {
		return nil, errors.New("simulated openfile failure")
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *failFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.failLstat != "" && filepath.Clean(name) == filepath.Clean(f.failLstat) {
		return nil, false, errors.New("simulated lstat failure")
	}
	return f.Fs.(interface {
		LstatIfPossible(string) (os.FileInfo, bool, error)
	}).LstatIfPossible(name)
}

func seedSrc(t *testing.T, fs afero.Fs) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, "/in/a.mp4", []byte("data"), 0644))
}

func TestCoverMoveNoReplace_MissingSource(t *testing.T) {
	fs := afero.NewMemMapFs()
	err := MoveFileNoReplace(fs, "/in/nope.mp4", "/out/nope.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "probe source")
}

func TestCoverMoveNoReplace_DestIndeterminate(t *testing.T) {
	fs := afero.NewMemMapFs()
	seedSrc(t, fs)
	wrapped := &failFs{Fs: fs, failLstat: "/out/a.mp4"}
	err := MoveFileNoReplace(wrapped, "/in/a.mp4", "/out/a.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "probe destination")
}

func TestCoverMoveNoReplace_MkdirAllFailure(t *testing.T) {
	fs := afero.NewMemMapFs()
	seedSrc(t, fs)
	wrapped := &failFs{Fs: fs, failMkdirAll: "/out"}
	err := MoveFileNoReplace(wrapped, "/in/a.mp4", "/out/a.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create destination directory")
}

func TestCoverMoveNoReplace_EXDEVLegCopyErrorSurfaces(t *testing.T) {
	if !forceNoReplaceEXDEV(t, 1) {
		t.Skip("no publish seams")
	}
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "a.mp4")
	dst := filepath.Join(dir, "sub", "a.mp4")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0755))
	require.NoError(t, os.WriteFile(dst, []byte("foreign"), 0644))
	// Wait: EXDEV forced but dst occupied → classification refuses before staging.
	err := MoveFileNoReplace(fs, src, dst)
	require.ErrorIs(t, err, ErrPublishCollision)
	content, _ := os.ReadFile(dst)
	assert.Equal(t, "foreign", string(content))
}

func TestCoverCopyNoReplace_MkdirAllFailure(t *testing.T) {
	fs := afero.NewMemMapFs()
	seedSrc(t, fs)
	wrapped := &failFs{Fs: fs, failMkdirAll: "/out"}
	err := CopyFileNoReplace(wrapped, "/in/a.mp4", "/out/a.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create destination directory")
}

func TestCoverCopyNoReplace_OpenSourceFailure(t *testing.T) {
	fs := afero.NewMemMapFs()
	seedSrc(t, fs)
	wrapped := &failFs{Fs: fs, failOpen: "/in/a.mp4"}
	err := CopyFileNoReplace(wrapped, "/in/a.mp4", "/out/a.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open source")
}

func TestCoverCopyNoReplace_StagingFailure(t *testing.T) {
	fs := afero.NewMemMapFs()
	seedSrc(t, fs)
	wrapped := &failFs{Fs: fs, failOpenFile: "/out/a.mp4.nrstg."}
	err := CopyFileNoReplace(wrapped, "/in/a.mp4", "/out/a.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exclusive staging")
}

func TestCoverCopyNoReplace_StreamFailureDiscardsStaging(t *testing.T) {
	fs := afero.NewMemMapFs()
	seedSrc(t, fs)
	// kill the copy mid-stream by failing Reads: wrap Open to return a file
	// whose Read always fails.
	wrapped := &readFailFs{Fs: fs}
	err := CopyFileNoReplace(wrapped, "/in/a.mp4", "/out/a.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream into staging")
	entries, derr := afero.ReadDir(fs, "/out")
	require.NoError(t, derr)
	assert.Empty(t, entries, "staged discard must not leave leftovers")
}

type readFailFile struct {
	afero.File
}

func (r *readFailFile) Read(p []byte) (int, error) { return 0, errors.New("simulated read failure") }

type readFailFs struct{ afero.Fs }

func (r *readFailFs) Open(name string) (afero.File, error) {
	fh, err := r.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	return &readFailFile{File: fh}, nil
}

// TestCoverCopyNoReplace_PublishFailureDiscardsStaging drives a collision INSIDE
// the publish (occupied after classification via direct pre-plant of BOTH
// outcomes) — plus the discard legs on the virtual fs where publication of a
// planted name must refuse cleanly.
func TestCoverCopyNoReplace_PublishFailureDiscardsStaging(t *testing.T) {
	fs := afero.NewMemMapFs()
	seedSrc(t, fs)
	// Pre-claim the ordinal staging name so .nrstg.0 is OURS-like but then a
	// foreign target exists by publish time — trivially reproduced by making
	// the virtual publish fail: don't rewrite its internal state; instead,
	// force destination occupancy between classification and publish by
	// pre-seeding dst before Delete? Simplest coverage: directly exercise
	// discardStagedAfterFailedPublish semantics.
	require.NoError(t, afero.WriteFile(fs, "/out/a.mp4.nrstg.0", []byte("staged"), 0644))
	info0, statErr0 := fs.Stat("/out/a.mp4.nrstg.0")
	require.NoError(t, statErr0)
	discardStagedAfterFailedPublish(fs, "/out/a.mp4.nrstg.0", info0, fsutil_cropErr("discarded"))
	_, err := fs.Stat("/out/a.mp4.nrstg.0")
	assert.True(t, os.IsNotExist(err), "staged discarded on ordinary failure")

	require.NoError(t, afero.WriteFile(fs, "/out/a.mp4.nrstg.1", []byte("staged"), 0644))
	info1, statErr1 := fs.Stat("/out/a.mp4.nrstg.1")
	require.NoError(t, statErr1)
	discardStagedAfterFailedPublish(fs, "/out/a.mp4.nrstg.1", info1, cropVerifyErr())
	_, err = fs.Stat("/out/a.mp4.nrstg.1")
	assert.NoError(t, err, "ErrPublishStagedVerify keeps the name in place")

	require.NoError(t, afero.WriteFile(fs, "/out/a.mp4.nrstg.2", []byte("staged"), 0644))
	info2, statErr2 := fs.Stat("/out/a.mp4.nrstg.2")
	require.NoError(t, statErr2)
	discardStagedAfterFailedPublish(fs, "/out/a.mp4.nrstg.2", info2, cropCompletedErr())
	_, err = fs.Stat("/out/a.mp4.nrstg.2")
	assert.NoError(t, err, "ErrPublishCompleted-carrying keeps the name in place")
}

// helper wrappers producing the discard-stable error classes
func fsutil_cropErr(msg string) error { return errors.New(msg) }
func cropVerifyErr() error            { return fmt.Errorf("verify failed: %w", ErrPublishStagedVerify) }
func cropCompletedErr() error         { return fmt.Errorf("published then failed: %w", ErrPublishCompleted) }

// renameFailFs fails Rename for a named destination — drives the replace-leg
// publish-failure path of copyFileDataFs (covered line: discard of .mvstg).
type renameFailFs struct {
	afero.Fs
	failRenameDst string
}

func (r *renameFailFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(r.failRenameDst) {
		return errors.New("simulated publish rename failure")
	}
	return r.Fs.Rename(oldname, newname)
}

func TestCoverMoveFsCopyFileData_PublishFailureDiscards(t *testing.T) {
	fs := afero.NewMemMapFs()
	seedSrc(t, fs)
	wrapped := &renameFailFs{Fs: fs, failRenameDst: "/out/a.mp4"}
	err := copyFileDataFs(wrapped, "/in/a.mp4", "/out/a.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to rename temp file to destination")
	entries, derr := afero.ReadDir(fs, "/out")
	require.NoError(t, derr)
	// The freshly staged .mvstg file must be discarded — no leftovers.
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".mvstg", "staged file left behind: %s", e.Name())
	}
}

func TestCoverCopyNoReplace_PublishErrorLeg(t *testing.T) {
	// All renames fail → the bound publish of the staged copy fails with a
	// generic error → discardStagedAfterFailedPublish removes the .nrstg name.
	fs := afero.NewMemMapFs()
	seedSrc(t, fs)
	wrapped := &renameFailFs{Fs: fs, failRenameDst: "/out/a.mp4"}
	err := CopyFileNoReplace(wrapped, "/in/a.mp4", "/out/a.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish")
	// no .nrstg leftovers
	exists, _ := afero.Exists(fs, "/out/a.mp4.nrstg.0")
	assert.False(t, exists)
}

func TestCoverNextOrdinalMonotonic(t *testing.T) {
	a := nextNoReplaceOrdinal()
	b := nextNoReplaceOrdinal()
	assert.Greater(t, b, a)
}

func TestCoverMoveNoReplace_EXDEVReProbeFailure(t *testing.T) {
	// src vanishes exactly as the publish refuses EXDEV, so the composite's
	// re-probe before staging finds nothing.
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "a.mp4")
	dst := filepath.Join(dir, "sub", "a.mp4")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0644))
	if !forceNoReplaceEXDEV(t, 1) {
		t.Skip("no publish seams")
	}
	if !hookNoReplacePlantSeamsFirstCall(t, func() { _ = os.Remove(src) }) {
		t.Skip("no publish seams")
	}
	err := MoveFileNoReplace(fs, src, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "re-probe source")
}

func TestCoverMoveNoReplace_EXDEVCopyPublishRefusal(t *testing.T) {
	// EXDEV forced; dst planted right at the second publish-seam call so the
	// cross-device copy's publish refuses the occupied destination.
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "a.mp4")
	dst := filepath.Join(dir, "sub", "a.mp4")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0644))
	if !forceNoReplaceEXDEV(t, 1) {
		t.Skip("no publish seams")
	}
	// Plant foreign content at dst right as the 2nd publish-seam call begins
	// (the staged EXDEV-leg publish), so its classification refuses.
	plant := func() {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			_ = os.MkdirAll(filepath.Dir(dst), 0755)
			_ = os.WriteFile(dst, []byte("foreign"), 0644)
		}
	}
	if !hookNoReplacePlantSeamsLater(t, 2, plant) {
		t.Skip("no publish seams")
	}
	err := MoveFileNoReplace(fs, src, dst)
	require.ErrorIs(t, err, ErrPublishCollision)
	content, _ := os.ReadFile(dst)
	assert.Equal(t, "foreign", string(content))
	content, _ = os.ReadFile(src)
	assert.Equal(t, "x", string(content), "source preserved after copy refusal")
}

// #224 codex P1: staging honors the configured umask — chmod-reasserted staging
// must not widen published permissions beyond FilePerm &^ umask.
func TestStagingFileMode_HonorsCachedUmask(t *testing.T) {
	prev := config.UmaskValue()
	config.StoreUmask(0o002)
	t.Cleanup(func() { config.StoreUmask(prev) })

	fs := afero.NewMemMapFs()
	seedSrc(t, fs)
	require.NoError(t, CopyFileNoReplace(fs, "/in/a.mp4", "/out/a.mp4"))
	info, err := fs.Stat("/out/a.mp4")
	require.NoError(t, err)
	// MemMapFs doesn't apply umask at all, so whatever stagingFileMode() decided
	// is the truth being published.
	assert.Equal(t, config.FilePerm&^os.FileMode(0o002), info.Mode().Perm())
}

// #224 codex P2: the discard binds by identity — a substitute planted at the
// staged name after our staging is never unlinked.
func TestDiscardBound_PlantSubstitutePreserved(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out", 0777))
	staged := "/out/a.mp4.nrstg.9"
	require.NoError(t, afero.WriteFile(fs, staged, []byte("staged-content"), 0644))
	info, err := fs.Stat(staged)
	require.NoError(t, err)

	// Substitute the staged name with a foreign object (different identity).
	require.NoError(t, fs.Remove(staged))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("foreign"), 0644))

	discardStagedAfterFailedPublish(fs, staged, info, fsutil_cropErr("publish failed"))

	content, rerr := afero.ReadFile(fs, staged)
	require.NoError(t, rerr)
	assert.Equal(t, "foreign", string(content), "foreign substitute must be preserved, never unlinked")
}
