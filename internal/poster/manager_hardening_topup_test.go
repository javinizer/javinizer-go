package poster

// Patch-coverage top-up for the hardening legs the PR's exercise suite did
// not reach: cache-untouched classification plumbing (Unwrap), CropWithBounds'
// own staging/.bak jam legs, and DownloadFromURL's pre-mutation temp-file
// failures (all positively marked cache-untouched).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCacheUntouchedError_UnwrapPreservesCause pins the dual classifier:
// errors.Is reaches ErrPosterCacheUntouched via the custom Is AND the
// underlying cause via Unwrap — sanitizers that unwrap must see the cause.
func TestCacheUntouchedError_UnwrapPreservesCause(t *testing.T) {
	cause := errors.New("dns exploded")
	err := markCacheUntouched(fmt.Errorf("SSRF validation failed: %w", cause))

	assert.ErrorIs(t, err, ErrPosterCacheUntouched)
	assert.ErrorIs(t, err, cause, "Unwrap must expose the underlying cause")
	assert.EqualError(t, err, "SSRF validation failed: dns exploded",
		"the wrapper must not alter the message")
}

// failSuffixRemoveFs fails Remove calls for a name suffix with a non-NotExist
// error, driving CropWithBounds' stale-staging sweep failure leg.
type failSuffixRemoveFs struct {
	afero.Fs
	suffix string
	err    error
}

func (f *failSuffixRemoveFs) Remove(name string) error {
	if strings.HasSuffix(filepath.ToSlash(name), f.suffix) {
		return f.err
	}
	return f.Fs.Remove(name)
}

// TestCropWithBounds_StaleStagingSweepFailureFailsClosed pins the
// "failed to clear stale poster preview staging" leg: a Remove error that is
// NOT not-exist on this crop's own {posterID}.jpg.tmp aborts the crop before
// anything else is staged or cropped.
func TestCropWithBounds_StaleStagingSweepFailureFailsClosed(t *testing.T) {
	mem := afero.NewMemMapFs()
	fs := &failSuffixRemoveFs{Fs: mem, suffix: ".jpg.tmp", err: errors.New("forced staging sweep failure")}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", nil)

	const jobID = "job-sweep1"
	const posterID = "SWP-001"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	require.NoError(t, createTestJPEG(fs, filepath.Join(dir, posterID+"-full.jpg"), 800, 500))
	preview := []byte("existing preview bytes")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, posterID+".jpg"), preview, 0o644))

	_, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clear stale poster preview staging")

	got, readErr := afero.ReadFile(mem, filepath.Join(dir, posterID+".jpg"))
	require.NoError(t, readErr, "the failed sweep must abort before touching the live preview")
	assert.Equal(t, preview, got)
}

// failDstSuffixRenameFs fails Rename calls whose DESTINATION ends in a
// suffix: ".jpg.bak" jams the stage-aside leg; the preview path jams the
// install rename AND its rollback rename alike.
type failDstSuffixRenameFs struct {
	afero.Fs
	suffix string
	err    error
}

func (f *failDstSuffixRenameFs) Rename(oldPath, newPath string) error {
	if strings.HasSuffix(filepath.ToSlash(newPath), f.suffix) {
		return f.err
	}
	return f.Fs.Rename(oldPath, newPath)
}

// TestCropWithBounds_StageAsideFailureKeepsLivePreview pins the
// "failed to stage existing poster preview aside" leg: an existing preview
// whose dest→.bak rename fails must abort the crop with the live preview
// still in place and the staged crop dropped.
func TestCropWithBounds_StageAsideFailureKeepsLivePreview(t *testing.T) {
	mem := afero.NewMemMapFs()
	fs := &failDstSuffixRenameFs{Fs: mem, suffix: "/" + "STG-001.jpg.bak", err: errors.New("forced stage-aside failure")}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", nil)

	const jobID = "job-stage1"
	const posterID = "STG-001"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	require.NoError(t, createTestJPEG(fs, filepath.Join(dir, posterID+"-full.jpg"), 800, 500))
	preview := []byte("existing preview bytes")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, posterID+".jpg"), preview, 0o644))

	_, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stage existing poster preview aside")

	got, readErr := afero.ReadFile(mem, filepath.Join(dir, posterID+".jpg"))
	require.NoError(t, readErr, "a failed stage-aside must leave the live preview untouched")
	assert.Equal(t, preview, got)
	_, statErr := mem.Stat(filepath.Join(dir, posterID+".jpg.tmp"))
	assert.Error(t, statErr, "the staged crop must be cleaned up on the stage-aside failure leg")
}

// TestCropWithBounds_InstallAndRollbackFailureSurfacesBoth pins the joined
// leg: the install rename (staging → preview) fails and rolling the .bak
// back onto the preview ALSO fails — both errors must ride the returned
// error, and the only copy of the old preview stays recoverable in .bak.
func TestCropWithBounds_InstallAndRollbackFailureSurfacesBoth(t *testing.T) {
	mem := afero.NewMemMapFs()
	// Jamming by DESTINATION name {posterID}.jpg fails the install rename
	// (staging → preview) and the rollback rename (.bak → preview) alike,
	// while the stage-aside rename (preview → .bak) goes through.
	fs := &failDstSuffixRenameFs{Fs: mem, suffix: "/" + "DBL-001.jpg", err: errors.New("forced preview-destination rename failure")}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", nil)

	const jobID = "job-dbl1"
	const posterID = "DBL-001"
	dir := filepath.Join("/tmp/javinizer-test", "posters", jobID)
	require.NoError(t, createTestJPEG(fs, filepath.Join(dir, posterID+"-full.jpg"), 800, 500))
	preview := []byte("only copy of the old preview")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, posterID+".jpg"), preview, 0o644))

	_, err := pm.CropWithBounds(context.Background(), jobID, posterID, 10, 20, 300, 400, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install cropped poster preview")
	assert.Contains(t, err.Error(), "poster preview rollback failed",
		"the failed .bak rollback must join the install error, not be swallowed")

	got, readErr := afero.ReadFile(mem, filepath.Join(dir, posterID+".jpg.bak"))
	require.NoError(t, readErr, "the only copy of the old preview must survive in .bak")
	assert.Equal(t, preview, got)
}

// failTempCreateFs fails the OpenFile(O_EXCL) that afero.TempFile issues for
// DownloadFromURL's staging file, without breaking any other create.
type failTempCreateFs struct {
	afero.Fs
	err error
}

func (f *failTempCreateFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.Contains(name, "-full-") {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// failCloseFile wraps a file whose first Close fails (writes stay healthy),
// driving DownloadFromURL's temp-file closeErr leg.
type failCloseFile struct {
	afero.File
	err    error
	closed bool
}

func (w *failCloseFile) Close() error {
	if !w.closed {
		w.closed = true
		return w.err
	}
	return w.File.Close()
}

// failCloseTempCreateFs hands failCloseFile out for the temp-download
// OpenFile(O_EXCL), leaving the copy healthy but the close poisoned.
type failCloseTempCreateFs struct {
	afero.Fs
	err error
}

func (f *failCloseTempCreateFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if flag&os.O_EXCL != 0 && strings.Contains(name, "-full-") {
		return &failCloseFile{File: file, err: f.err}, nil
	}
	return file, nil
}

// errBody is an http.Response body whose Read fails immediately, driving
// DownloadFromURL's io.Copy "failed to write image" leg.
type errBody struct{ err error }

func (b errBody) Read([]byte) (int, error) { return 0, b.err }
func (b errBody) Close() error             { return nil }

// stubHTTPClient returns a canned response for the download request.
type stubHTTPClient struct {
	resp *http.Response
	err  error
}

func (c stubHTTPClient) Do(_ *http.Request) (*http.Response, error) { return c.resp, c.err }

func okImageResponse(body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}
}

// seedExistingPosterCache writes the pre-download full image and preview that
// every pre-mutation failure leg must leave byte-for-byte intact.
func seedExistingPosterCache(t *testing.T, fs afero.Fs, tempDir, jobID, posterID string) (dir string, full []byte) {
	t.Helper()
	dir = filepath.Join(tempDir, "posters", jobID)
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	full = []byte("existing full image")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, posterID+"-full.jpg"), full, 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, posterID+".jpg"), []byte("existing preview"), 0o644))
	return dir, full
}

// TestDownloadFromURL_TempFileCreateFailureMarkedCacheUntouched pins the
// "failed to create temp file" leg: pre-mutation, so the failure is
// positively marked ErrPosterCacheUntouched and the existing cache survives.
func TestDownloadFromURL_TempFileCreateFailureMarkedCacheUntouched(t *testing.T) {
	mem := afero.NewMemMapFs()
	fs := &failTempCreateFs{Fs: mem, err: errors.New("forced temp create failure")}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", stubHTTPClient{
		resp: okImageResponse(io.NopCloser(strings.NewReader("jpeg bytes"))),
	}).WithSSRFCheck(func(string) error { return nil })

	dir, oldFull := seedExistingPosterCache(t, mem, "/tmp/javinizer-test", "job-dltmp1", "DLT-001")

	_, err := pm.DownloadFromURL(context.Background(), "job-dltmp1", "DLT-001", "http://example.com/new.jpg", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create temp file")
	assert.ErrorIs(t, err, ErrPosterCacheUntouched)

	got, readErr := afero.ReadFile(mem, filepath.Join(dir, "DLT-001-full.jpg"))
	require.NoError(t, readErr)
	assert.Equal(t, oldFull, got, "a pre-mutation failure must never touch the existing cache")
}

// TestDownloadFromURL_BodyReadFailureMarkedCacheUntouched pins the io.Copy
// "failed to write image" leg — still pre-mutation (only the staging temp
// file exists), so it is marked cache-untouched and the staging file is
// swept.
func TestDownloadFromURL_BodyReadFailureMarkedCacheUntouched(t *testing.T) {
	mem := afero.NewMemMapFs()
	pm := NewPosterManager(mem, "/tmp/javinizer-test", stubHTTPClient{
		resp: okImageResponse(errBody{err: errors.New("connection reset mid-body")}),
	}).WithSSRFCheck(func(string) error { return nil })

	dir, oldFull := seedExistingPosterCache(t, mem, "/tmp/javinizer-test", "job-dlbody1", "DLB-001")

	_, err := pm.DownloadFromURL(context.Background(), "job-dlbody1", "DLB-001", "http://example.com/new.jpg", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write image")
	assert.ErrorIs(t, err, ErrPosterCacheUntouched)

	got, readErr := afero.ReadFile(mem, filepath.Join(dir, "DLB-001-full.jpg"))
	require.NoError(t, readErr)
	assert.Equal(t, oldFull, got)
	entries, readDirErr := afero.ReadDir(mem, dir)
	require.NoError(t, readDirErr)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), ".tmp"), "the staging temp must be swept on the write-failure leg")
	}
}

// TestDownloadFromURL_TempFileCloseFailureMarkedCacheUntouched pins the
// closeErr leg: the copy succeeded but Close failed — still pre-mutation,
// still marked cache-untouched, staging temp swept.
func TestDownloadFromURL_TempFileCloseFailureMarkedCacheUntouched(t *testing.T) {
	mem := afero.NewMemMapFs()
	fs := &failCloseTempCreateFs{Fs: mem, err: errors.New("forced temp close failure")}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", stubHTTPClient{
		resp: okImageResponse(io.NopCloser(strings.NewReader("jpeg bytes"))),
	}).WithSSRFCheck(func(string) error { return nil })

	dir, oldFull := seedExistingPosterCache(t, mem, "/tmp/javinizer-test", "job-dlclose1", "DLC-001")

	_, err := pm.DownloadFromURL(context.Background(), "job-dlclose1", "DLC-001", "http://example.com/new.jpg", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close temp file")
	assert.ErrorIs(t, err, ErrPosterCacheUntouched)

	got, readErr := afero.ReadFile(mem, filepath.Join(dir, "DLC-001-full.jpg"))
	require.NoError(t, readErr)
	assert.Equal(t, oldFull, got)
}
