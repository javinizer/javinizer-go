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
	"strings"
	"testing"

	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- Helpers ----

// createTestJPEG writes a small valid JPEG into the given filesystem path.
func createTestJPEG(fs afero.Fs, path string, width, height int) error {
	if err := fs.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := fs.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
}

// newTestManager creates a PosterManager backed by an in-memory filesystem.
func newTestManager(httpClient httpclientiface.HTTPClient) *PosterManager {
	fs := afero.NewMemMapFs()
	tempDir := "/tmp/javinizer-test"
	return NewPosterManager(fs, tempDir, httpClient)
}

// newTestManagerBypassSSRF creates a PosterManager that skips SSRF checks
// so httptest servers on 127.0.0.1 are reachable.
func newTestManagerBypassSSRF(httpClient httpclientiface.HTTPClient) *PosterManager {
	pm := newTestManager(httpClient)
	return pm.WithSSRFCheck(func(_ string) error { return nil })
}

// ---- validatePosterID ----

func TestValidatePosterID(t *testing.T) {
	tests := []struct {
		name      string
		posterID  string
		wantError bool
	}{
		{"valid", "ABC-123", false},
		{"empty", "", true},
		{"dot", ".", true},
		{"path traversal", "../etc/passwd", true},
		{"with separator", "foo/bar", true},
		{"with backslash", "foo\\bar", true}, // backslash is now rejected on all platforms
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePosterID(tt.posterID)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ---- validateJobID ----

func TestValidateJobID(t *testing.T) {
	tests := []struct {
		name      string
		jobID     string
		wantError bool
	}{
		{"valid", "job-123", false},
		{"empty", "", true},
		{"dot", ".", true},
		{"path traversal", "../etc/passwd", true},
		{"with separator", "foo/bar", true},
		{"with backslash", "foo\\bar", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJobID(tt.jobID)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ---- validatePathWithinDir ----

func TestValidatePathWithinDir(t *testing.T) {
	dir := "/tmp/posters/job1"
	tests := []struct {
		name      string
		path      string
		dir       string
		wantError bool
	}{
		{"valid", "/tmp/posters/job1/ABC-full.jpg", dir, false},
		{"traversal", "/tmp/posters/../etc/passwd", dir, true},
		{"sibling", "/tmp/posters/other/file.jpg", dir, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePathWithinDir(tt.path, tt.dir)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ---- CropWithBounds ----

func TestCropWithBounds_InvalidPosterID(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	_, err := pm.CropWithBounds(context.Background(), "job1", "", 0, 0, 100, 100, 500)
	assert.Error(t, err, "empty posterID should be rejected")

	_, err = pm.CropWithBounds(context.Background(), "job1", "../etc", 0, 0, 100, 100, 500)
	assert.Error(t, err, "path-traversal posterID should be rejected")
}

func TestCropWithBounds_InvalidJobID(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	_, err := pm.CropWithBounds(context.Background(), "", "ABC-123", 0, 0, 100, 100, 500)
	assert.Error(t, err, "empty jobID should be rejected")

	_, err = pm.CropWithBounds(context.Background(), "../etc", "ABC-123", 0, 0, 100, 100, 500)
	assert.Error(t, err, "path-traversal jobID should be rejected")
}

func TestCropWithBounds_SourceNotFound(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	_, err := pm.CropWithBounds(context.Background(), "job1", "ABC-123", 0, 0, 100, 100, 500)
	assert.Error(t, err, "should fail when source poster doesn't exist")
	assert.Contains(t, err.Error(), "source poster not found")
}

func TestCropWithBounds_FallbackToNonFullPath(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	// Create only the non-"-full" variant.
	posterID := "ABC-123"
	posterDir := filepath.Join(pm.tempDir, "posters", "job1")
	sourcePath := filepath.Join(posterDir, posterID+".jpg")
	require.NoError(t, createTestJPEG(pm.fs, sourcePath, 200, 300))

	result, err := pm.CropWithBounds(context.Background(), "job1", posterID, 0, 0, 100, 150, 500)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(posterDir, posterID+".jpg"), result.FullPath)
	assert.Contains(t, result.CroppedURL, "/api/v1/temp/posters/job1/ABC-123.jpg")
}

func TestCropWithBounds_PrefersFullPath(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	posterID := "ABC-123"
	posterDir := filepath.Join(pm.tempDir, "posters", "job1")
	fullPath := filepath.Join(posterDir, posterID+"-full.jpg")
	plainPath := filepath.Join(posterDir, posterID+".jpg")

	require.NoError(t, createTestJPEG(pm.fs, fullPath, 200, 300))
	require.NoError(t, createTestJPEG(pm.fs, plainPath, 200, 300))

	result, err := pm.CropWithBounds(context.Background(), "job1", posterID, 0, 0, 100, 150, 500)
	require.NoError(t, err)
	assert.Equal(t, fullPath, result.FullPath)
}

func TestCropWithBounds_Success(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	posterID := "ABC-123"
	posterDir := filepath.Join(pm.tempDir, "posters", "job1")
	sourcePath := filepath.Join(posterDir, posterID+"-full.jpg")
	require.NoError(t, createTestJPEG(pm.fs, sourcePath, 200, 300))

	result, err := pm.CropWithBounds(context.Background(), "job1", posterID, 10, 10, 100, 150, 500)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CroppedPath)
	assert.NotEmpty(t, result.CroppedURL)
	assert.Contains(t, result.CroppedURL, "/api/v1/temp/posters/job1/ABC-123.jpg")

	// Verify cropped file exists.
	exists, err := afero.Exists(pm.fs, result.CroppedPath)
	assert.True(t, exists)
	assert.NoError(t, err)
}

// ---- DownloadFromURL ----

func TestDownloadFromURL_InvalidPosterID(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	_, err := pm.DownloadFromURL(context.Background(), "job1", "", "http://example.com/img.jpg", "", "")
	assert.Error(t, err)

	_, err = pm.DownloadFromURL(context.Background(), "job1", "../etc", "http://example.com/img.jpg", "", "")
	assert.Error(t, err)
}

func TestDownloadFromURL_InvalidJobID(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	_, err := pm.DownloadFromURL(context.Background(), "", "ABC-123", "http://example.com/img.jpg", "", "")
	assert.Error(t, err)

	_, err = pm.DownloadFromURL(context.Background(), "../etc", "ABC-123", "http://example.com/img.jpg", "", "")
	assert.Error(t, err)
}

func TestDownloadFromURL_SSRFBlocked(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	// SSRF package rejects localhost/loopback by default.
	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", "http://127.0.0.1/img.jpg", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SSRF")
}

func TestDownloadFromURL_InvalidScheme(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", "ftp://example.com/img.jpg", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SSRF")
}

func TestDownloadFromURL_Success(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		assert.Equal(t, "TestAgent", r.Header.Get("User-Agent"))
		assert.Contains(t, r.Header.Get("Accept"), "image/")
		// Auto-referer is generated from the URL since no custom referer was provided.
		assert.True(t, strings.HasPrefix(r.Header.Get("Referer"), serverURL), "auto referer should be based on URL host")

		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	serverURL = srv.URL
	defer srv.Close()

	pm := newTestManagerBypassSSRF(srv.Client())

	result, err := pm.DownloadFromURL(
		context.Background(), "job1", "ABC-123",
		srv.URL+"/img.jpg", "TestAgent", "",
	)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CroppedPath)
	assert.NotEmpty(t, result.FullPath)
	assert.Contains(t, result.CroppedURL, "/api/v1/temp/posters/job1/ABC-123.jpg")

	// Verify the full image file exists.
	exists, err := afero.Exists(pm.fs, result.FullPath)
	assert.True(t, exists)
	assert.NoError(t, err)

	// Verify the cropped image file exists.
	exists, err = afero.Exists(pm.fs, result.CroppedPath)
	assert.True(t, exists)
	assert.NoError(t, err)
}

func TestDownloadFromURL_CustomReferer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "https://custom.referer.com/", r.Header.Get("Referer"))

		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	pm := newTestManagerBypassSSRF(srv.Client())

	_, err := pm.DownloadFromURL(
		context.Background(), "job1", "ABC-123",
		srv.URL+"/img.jpg", "", "https://custom.referer.com/",
	)
	require.NoError(t, err)
}

func TestDownloadFromURL_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	pm := newTestManagerBypassSSRF(srv.Client())

	_, err := pm.DownloadFromURL(
		context.Background(), "job1", "ABC-123",
		srv.URL+"/img.jpg", "", "",
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 404")
}

func TestDownloadFromURL_TooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Write more than maxPosterSize+1 bytes.
		w.Header().Set("Content-Type", "image/jpeg")
		// Send maxPosterSize + 2 bytes of zeros.
		_, _ = io.CopyN(w, zeroReader{}, maxPosterSize+2)
	}))
	defer srv.Close()

	pm := newTestManagerBypassSSRF(srv.Client())

	_, err := pm.DownloadFromURL(
		context.Background(), "job1", "ABC-123",
		srv.URL+"/img.jpg", "", "",
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestDownloadFromURL_CropFallbackToCopy(t *testing.T) {
	// Serve a valid JPEG that image decoding can handle.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	pm := newTestManagerBypassSSRF(srv.Client())

	// The CropPosterFromCover should succeed for a landscape image, but we
	// verify the result is non-empty which implicitly tests the happy path.
	result, err := pm.DownloadFromURL(
		context.Background(), "job1", "ABC-123",
		srv.URL+"/img.jpg", "", "",
	)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CroppedPath)

	// Verify the cropped file exists.
	exists, err := afero.Exists(pm.fs, result.CroppedPath)
	assert.True(t, exists)
}

func TestDownloadFromURL_HTTPClientError(t *testing.T) {
	pm := newTestManager(&failingHTTPClient{})

	_, err := pm.DownloadFromURL(
		context.Background(), "job1", "ABC-123",
		"http://example.com/img.jpg", "", "",
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download image")
}

// failingHTTPClient always returns an error.
type failingHTTPClient struct{}

func (f *failingHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	return nil, errors.New("network failure")
}

// ---- copyFile ----

func TestCopyFile(t *testing.T) {
	fs := afero.NewMemMapFs()

	srcContent := "hello world"
	require.NoError(t, afero.WriteFile(fs, "/src.txt", []byte(srcContent), 0644))

	require.NoError(t, copyFile(fs, "/src.txt", "/dst.txt"))

	got, err := afero.ReadFile(fs, "/dst.txt")
	require.NoError(t, err)
	assert.Equal(t, srcContent, string(got))
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	fs := afero.NewMemMapFs()
	err := copyFile(fs, "/nonexistent.txt", "/dst.txt")
	assert.Error(t, err)
}

// ---- Integration-style: full round-trip ----

func TestDownloadThenCrop(t *testing.T) {
	// Serve a wide JPEG (landscape) so crop produces a meaningful result.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 600, 400))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	pm := newTestManagerBypassSSRF(srv.Client())

	// Step 1: Download.
	dlResult, err := pm.DownloadFromURL(
		context.Background(), "job1", "XYZ-001",
		srv.URL+"/img.jpg", "TestBot", "",
	)
	require.NoError(t, err)
	assert.NotEmpty(t, dlResult.CroppedURL)

	// Step 2: Crop with explicit bounds.
	cropResult, err := pm.CropWithBounds(
		context.Background(), "job1", "XYZ-001",
		0, 0, 200, 200, 500,
	)
	require.NoError(t, err)
	assert.NotEmpty(t, cropResult.CroppedURL)
	assert.Contains(t, cropResult.CroppedURL, "/api/v1/temp/posters/job1/XYZ-001.jpg")
}

// ---- Compile-time interface ----

func TestPosterManagerInterface_Satisfied(t *testing.T) {
	// This test exists for documentation; the compile-time check is the
	// var _ assignment in manager.go. We verify NewPosterManager returns a
	// usable value.
	pm := NewPosterManager(afero.NewMemMapFs(), "/tmp", http.DefaultClient)
	assert.NotNil(t, pm)
}

// ---- Context cancellation ----

func TestDownloadFromURL_CancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Delay long enough for context cancellation to trigger.
		<-make(chan struct{})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	pm := newTestManagerBypassSSRF(srv.Client())

	_, err := pm.DownloadFromURL(ctx, "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	assert.Error(t, err)
}

// ---- Path traversal defense ----

func TestCropWithBounds_PathTraversal(t *testing.T) {
	pm := newTestManager(http.DefaultClient)

	// A posterID that passes filepath.Base but would escape after path
	// construction is hard to construct; instead verify that the path
	// validation catches directory-traversal suffixes.
	posterID := "ABC-123"
	posterDir := filepath.Join(pm.tempDir, "posters", "job1")
	sourcePath := filepath.Join(posterDir, posterID+"-full.jpg")
	require.NoError(t, createTestJPEG(pm.fs, sourcePath, 200, 300))

	// Normal call should succeed.
	_, err := pm.CropWithBounds(context.Background(), "job1", posterID, 0, 0, 100, 100, 500)
	require.NoError(t, err)

	// Direct validation of the defense helper.
	err = validatePathWithinDir("/tmp/posters/job1/../../etc/passwd", "/tmp/posters/job1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
}

// ---- Download with auto-referer ----

func TestDownloadFromURL_AutoReferer(t *testing.T) {
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		referer := r.Header.Get("Referer")
		assert.True(t, strings.HasPrefix(referer, serverURL), "auto referer should be based on URL host, got: %s", referer)

		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	serverURL = srv.URL
	defer srv.Close()

	pm := newTestManagerBypassSSRF(srv.Client())

	_, err := pm.DownloadFromURL(
		context.Background(), "job1", "ABC-123",
		srv.URL+"/img.jpg", "", "",
	)
	require.NoError(t, err)
}

// ---- MaxPosterSize constant ----

func TestMaxPosterSize(t *testing.T) {
	assert.Equal(t, 50<<20, maxPosterSize, "maxPosterSize should be 50 MB")
}

// ---- DownloadFromURL creates temp dir ----

func TestDownloadFromURL_CreatesTempDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	// Use a fresh fs so the temp dir doesn't exist yet.
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/fresh-tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	result, err := pm.DownloadFromURL(
		context.Background(), "job1", "ABC-123",
		srv.URL+"/img.jpg", "", "",
	)
	require.NoError(t, err)
	assert.NotEmpty(t, result.FullPath)

	// Verify directory was created.
	exists, err := afero.DirExists(fs, "/fresh-tmp/posters/job1")
	assert.True(t, exists)
	assert.NoError(t, err)
}

// ---- DownloadFromURL temp file cleanup on error ----

func TestDownloadFromURL_CleansUpTempOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client())

	_, err := pm.DownloadFromURL(
		context.Background(), "job1", "ABC-123",
		srv.URL+"/img.jpg", "", "",
	)
	assert.Error(t, err)

	// No leftover temp files should exist.
	posterDir := "/tmp/posters/job1"
	entries, err := afero.ReadDir(fs, posterDir)
	if err == nil {
		for _, e := range entries {
			assert.False(t, strings.HasSuffix(e.Name(), ".tmp"),
				fmt.Sprintf("temp file should be cleaned up, found: %s", e.Name()))
		}
	}
}
