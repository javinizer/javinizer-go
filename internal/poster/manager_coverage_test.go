package poster

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadFromURL_MkdirFailure(t *testing.T) {
	// Use a read-only fs so MkdirAll fails
	fs := afero.NewMemMapFs()
	fs = afero.NewReadOnlyFs(fs)
	pm := NewPosterManager(fs, "/tmp", http.DefaultClient).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", "http://example.com/img.jpg", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create temp poster directory")
}

func TestDownloadFromURL_InvalidURL(t *testing.T) {
	pm := newTestManagerBypassSSRF(http.DefaultClient)

	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", "://bad-url", "", "")
	assert.Error(t, err)
}

func TestCopyFile_DestinationCloseError(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/src.txt", []byte("hello"), 0644))

	err := copyFile(fs, "/src.txt", "/dst.txt")
	assert.NoError(t, err)

	got, err := afero.ReadFile(fs, "/dst.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(got))
}

func TestCopyFile_CreateDestinationFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	fs = afero.NewReadOnlyFs(fs)
	require.NoError(t, afero.WriteFile(afero.NewMemMapFs(), "/src.txt", []byte("hello"), 0644))

	err := copyFile(fs, "/src.txt", "/dst.txt")
	assert.Error(t, err)
}

func TestDownloadFromURL_SetsUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "MyBot/1.0", r.Header.Get("User-Agent"))
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	pm := newTestManagerBypassSSRF(srv.Client())
	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "MyBot/1.0", "")
	require.NoError(t, err)
}

func TestDownloadFromURL_CleanupOnCropFailure(t *testing.T) {
	// Serve a small valid JPEG so download succeeds, but crop should still work or fallback
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	pm := newTestManagerBypassSSRF(srv.Client())
	result, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, result.FullPath)
	assert.NotEmpty(t, result.CroppedPath)
}

func TestSanitizedError_NilReceiver_Error(t *testing.T) {
	var e *sanitizedError
	assert.Equal(t, "", e.Error())
}

func TestSanitizedError_NilReceiver_Unwrap(t *testing.T) {
	var e *sanitizedError
	assert.Nil(t, e.Unwrap())
}

func TestSanitizedError_NonNil_Error(t *testing.T) {
	e := &sanitizedError{sanitized: "safe message", cause: fmt.Errorf("original")}
	assert.Equal(t, "safe message", e.Error())
}

func TestSanitizedError_NonNil_Unwrap(t *testing.T) {
	cause := fmt.Errorf("original")
	e := &sanitizedError{sanitized: "safe", cause: cause}
	unwrapped := e.Unwrap()
	assert.Equal(t, cause, unwrapped)
}

func TestSanitizedErrorFrom_Nil(t *testing.T) {
	assert.Nil(t, sanitizedErrorFrom(nil))
}

func TestStripSensitivePaths_NilError(t *testing.T) {
	assert.Equal(t, "", stripSensitivePaths(nil))
}

func TestDownloadFromURL_HttpClientError_WithBypassSSRF(t *testing.T) {
	pm := newTestManagerBypassSSRF(&failingHTTPClient{})
	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", "http://example.com/img.jpg", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download image")
}

func TestDownloadFromURL_PathValidationEscapes(t *testing.T) {
	err := validatePathWithinDir("/etc/passwd", "/tmp/posters/job1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
}

func TestDrainAndClose_WithBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "some body content")
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	err = drainAndClose(resp.Body)
	assert.NoError(t, err)
}

func TestDownloadFromURL_RenamesTempFile(t *testing.T) {
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

	// Verify no .tmp files remain
	entries, err := afero.ReadDir(fs, "/tmp/posters/job1")
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), ".tmp"), "temp file should be renamed, found: %s", e.Name())
	}
	_ = result
}
