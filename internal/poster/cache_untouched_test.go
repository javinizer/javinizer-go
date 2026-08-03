package poster

import (
	"context"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pin the ErrPosterCacheUntouched classification contract: rescrape (and any
// other fail-closed caller) degrades failures whose positive mark proves the
// Remove-before-Rename mutation boundary was never crossed, and keeps failing
// closed on every failure at/past it.

func TestDownloadFromURL_PreMutationFailuresMarkedCacheUntouched(t *testing.T) {
	srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv404.Close()

	tests := []struct {
		name    string
		setup   func() *PosterManager
		rawURL  string
		wantMsg string
	}{
		{
			name:    "invalid poster ID",
			setup:   func() *PosterManager { return newTestManager(nil) },
			rawURL:  "https://example.com/img.jpg",
			wantMsg: "invalid poster ID",
		},
		{
			name:    "SSRF blocked",
			setup:   func() *PosterManager { return newTestManager(nil) },
			rawURL:  "http://127.0.0.1/img.jpg",
			wantMsg: "SSRF validation failed",
		},
		{
			name:    "network error",
			setup:   func() *PosterManager { return newTestManagerBypassSSRF(&failingHTTPClient{}) },
			rawURL:  "https://example.com/img.jpg",
			wantMsg: "failed to download image",
		},
		{
			name:    "HTTP 404",
			setup:   func() *PosterManager { return newTestManagerBypassSSRF(srv404.Client()) },
			rawURL:  srv404.URL + "/img.jpg",
			wantMsg: "status 404",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := tt.setup()
			posterID := "ABC-123"
			if tt.name == "invalid poster ID" {
				posterID = "../evil"
			}
			_, err := pm.DownloadFromURL(context.Background(), "job1", posterID, tt.rawURL, "", "")
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrPosterCacheUntouched,
				"a failure before the mutation boundary must carry the cache-untouched mark")
			assert.Contains(t, err.Error(), tt.wantMsg,
				"the mark must not alter the surfaced message")
		})
	}
}

// renameFailFS jams only the Rename leg so DownloadFromURL reaches the
// Remove-before-Rename mutation boundary and then fails at the finalize step.
type renameFailFS struct{ afero.Fs }

func (f *renameFailFS) Rename(_, _ string) error { return errors.New("rename jammed") }

func TestDownloadFromURL_FinalizeFailureNotMarkedCacheUntouched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	base := afero.NewMemMapFs()
	pm := NewPosterManager(&renameFailFS{Fs: base}, "/tmp/javinizer-test", srv.Client()).
		WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "ABC-123", srv.URL+"/img.jpg", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to finalize image download")
	assert.NotErrorIs(t, err, ErrPosterCacheUntouched,
		"a failure at/past the mutation boundary must stay unmarked so callers fail closed")
}

func TestMarkCacheUntouched_NilPassthrough(t *testing.T) {
	assert.Nil(t, markCacheUntouched(nil))
}
