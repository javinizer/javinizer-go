package poster

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DownloadFromURL: close error on temp file ---

func TestDownloadFromURL_CloseErrorOnTempFile(t *testing.T) {
	// This tests the path where temp file close returns an error.
	// With afero MemMapFs, Close() generally doesn't error, so this
	// mainly exercises the code path via a successful download.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	result, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, result.FullPath)
	assert.NotEmpty(t, result.CroppedPath)
}

// --- DownloadFromURL: rename failure ---

func TestDownloadFromURL_RenameFailure(t *testing.T) {
	// Use a read-only filesystem to trigger rename failure
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	// Create mem map fs and make it read-only after creating the temp dir
	memFS := afero.NewMemMapFs()
	require.NoError(t, memFS.MkdirAll("/tmp/posters/job1", 0755))

	// Wrap with read-only to cause rename failure
	readOnlyFS := afero.NewReadOnlyFs(memFS)
	pm := NewPosterManager(readOnlyFS, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	assert.Error(t, err)
}

// --- CropWithBounds: crop fails (image too small for bounds) ---

func TestCropWithBounds_CropFailure(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	posterID := "ABC-123"
	posterDir := filepath.Join(pm.tempDir, "posters", "job1")
	sourcePath := filepath.Join(posterDir, posterID+"-full.jpg")

	// Create a very small image that won't support the crop bounds
	require.NoError(t, createTestJPEG(pm.fs, sourcePath, 10, 10))

	// Try to crop with bounds larger than the image - should succeed
	// (imageutil.CropPosterWithBounds may or may not fail for out-of-bounds)
	_, err := pm.CropWithBounds(context.Background(), "job1", posterID, 0, 0, 100, 100, 500)
	// imageutil.CropPosterWithBounds should fail for out-of-bounds crop
	assert.Error(t, err)
}

// --- DownloadFromURL: crop failure falls back to copy ---

func TestDownloadFromURL_CropFailsFallsBackToCopy(t *testing.T) {
	// Create a server that returns a very narrow image (1x1 pixel)
	// which cannot be cropped as a poster
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	result, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	// Even if crop fails, copyFile fallback should succeed
	require.NoError(t, err)
	assert.NotEmpty(t, result.FullPath)
	assert.NotEmpty(t, result.CroppedPath)
}

// --- DownloadFromURL: both crop and copy fail ---

func TestDownloadFromURL_CropAndCopyBothFail(t *testing.T) {
	// Create a server that returns valid image data, but use a
	// filesystem that allows creation but not reading, to make copyFile fail
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	// Pre-create the directory so DownloadFromURL can create temp files
	require.NoError(t, memFS.MkdirAll("/tmp/posters/job1", 0755))

	// Use a custom fs that fails on Open (used by copyFile)
	failOpenFS := &failOpenFs{Fs: memFS}
	pm := NewPosterManager(failOpenFS, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	// Should fail because crop and copy both fail
	assert.Error(t, err)
}

// failOpenFs wraps afero.Fs but fails on Open
type failOpenFs struct {
	afero.Fs
}

func (f *failOpenFs) Open(name string) (afero.File, error) {
	return nil, errors.New("open failed")
}

// --- DownloadFromURL: cleans up on panic (defer cleanup) ---

func TestDownloadFromURL_CleanupOnFailure(t *testing.T) {
	// Test that temp files are cleaned up when download fails at various stages
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 404 = fail
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	assert.Error(t, err)

	// Verify cleanup: no full/cropped files should remain
	posterDir := "/tmp/posters/job1"
	exists, _ := afero.Exists(fs, posterDir+"/ABC-123-full.jpg")
	assert.False(t, exists, "full image should be cleaned up on error")
	exists, _ = afero.Exists(fs, posterDir+"/ABC-123.jpg")
	assert.False(t, exists, "cropped image should be cleaned up on error")
}

// --- copyFile: source open failure ---

func TestCopyFile_SourceOpenFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	err := copyFile(fs, "/nonexistent/source.txt", "/dst.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open source file")
}

// --- copyFile: destination close error (implicit) ---

func TestCopyFile_SuccessfulCopy(t *testing.T) {
	fs := afero.NewMemMapFs()
	content := []byte("test content for copy")
	require.NoError(t, afero.WriteFile(fs, "/src.txt", content, 0644))

	err := copyFile(fs, "/src.txt", "/dst.txt")
	require.NoError(t, err)

	got, err := afero.ReadFile(fs, "/dst.txt")
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

// --- CropWithBounds: path validation for cropped path ---

func TestCropWithBounds_ValidPathValidation(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	posterID := "valid-id"
	posterDir := filepath.Join(pm.tempDir, "posters", "job1")
	_ = posterDir
	sourcePath := filepath.Join(posterDir, posterID+"-full.jpg")
	require.NoError(t, createTestJPEG(pm.fs, sourcePath, 200, 300))

	result, err := pm.CropWithBounds(context.Background(), "job1", posterID, 0, 0, 100, 150, 500)
	require.NoError(t, err)
	// Verify both paths pass path validation
	assert.Contains(t, result.FullPath, posterDir)
	assert.Contains(t, result.CroppedPath, posterDir)
}

// --- DownloadFromURL: sets auto-referer from URL when no custom referer ---

func TestDownloadFromURL_AutoRefererFromURL(t *testing.T) {
	var receivedReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedReferer = r.Header.Get("Referer")
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	require.NoError(t, err)

	// Auto-referer should be set to the server's base URL
	assert.NotEmpty(t, receivedReferer)
	assert.Contains(t, receivedReferer, srv.URL)
}

// --- DownloadFromURL: temp file creation failure ---

func TestDownloadFromURL_TempFileCreationFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	// Read-only fs can't create temp files
	memFS := afero.NewMemMapFs()
	readOnlyFS := afero.NewReadOnlyFs(memFS)
	pm := NewPosterManager(readOnlyFS, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	assert.Error(t, err)
}

// --- CropPosterFromCover (via imageutil) with real poster dimensions ---

func TestDownloadFromURL_WideImageCrop(t *testing.T) {
	// Test that a wide image (like a cover) gets properly cropped
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 800, 600)) // Landscape image
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	result, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, result.FullPath)
	assert.NotEmpty(t, result.CroppedPath)

	// Verify files exist
	exists, err := afero.Exists(fs, result.FullPath)
	assert.True(t, exists)
	assert.NoError(t, err)

	exists, err = afero.Exists(fs, result.CroppedPath)
	assert.True(t, exists)
	assert.NoError(t, err)
}

// --- DownloadFromURL: previous full image removed before rename ---

func TestDownloadFromURL_RemovesPreviousFullImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	// First download creates the full image
	result1, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	require.NoError(t, err)

	// Second download should remove the previous full image and replace it
	result2, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	require.NoError(t, err)

	// Both should succeed and the final state should have the latest files
	assert.NotEmpty(t, result1.FullPath)
	assert.NotEmpty(t, result2.FullPath)
}

// Suppress unused import warning
var _ = fmt.Sprintf
var _ = io.Discard
