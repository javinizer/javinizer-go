package poster

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScrapePosterGenerator_NilManager(t *testing.T) {
	gen := NewScrapePosterGenerator(nil, "", "")
	movie := &models.Movie{ID: "TEST-001", Poster: models.PosterState{PosterURL: "https://example.com/poster.jpg"}}
	err := gen.GeneratePoster(context.Background(), "test-job", movie)
	assert.NoError(t, err)
}

func TestScrapePosterGenerator_NilMovie(t *testing.T) {
	gen := NewScrapePosterGenerator(nil, "", "")
	err := gen.GeneratePoster(context.Background(), "test-job", nil)
	assert.NoError(t, err)
}

func TestScrapePosterGenerator_NoPosterOrCoverURL(t *testing.T) {
	pm := NewPosterManager(afero.NewMemMapFs(), "/tmp", http.DefaultClient)
	gen := NewScrapePosterGenerator(pm, "", "")
	movie := &models.Movie{ID: "TEST-001"}
	err := gen.GeneratePoster(context.Background(), "test-job", movie)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no poster or cover URL available")
}

func TestScrapePosterGenerator_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })
	gen := NewScrapePosterGenerator(pm, "TestAgent", "")

	movie := &models.Movie{ID: "TEST-001", Poster: models.PosterState{PosterURL: srv.URL + "/poster.jpg"}}
	err := gen.GeneratePoster(context.Background(), "test-job", movie)
	require.NoError(t, err)
	assert.Contains(t, movie.Poster.CroppedPosterURL, "/api/v1/temp/posters/test-job/TEST-001.jpg")
	assert.False(t, movie.Poster.ShouldCropPoster)
}

func TestScrapePosterGenerator_UsesCoverURLFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })
	gen := NewScrapePosterGenerator(pm, "", "")

	movie := &models.Movie{ID: "TEST-002", Poster: models.PosterState{CoverURL: srv.URL + "/cover.jpg"}}
	err := gen.GeneratePoster(context.Background(), "test-job", movie)
	require.NoError(t, err)
	assert.Contains(t, movie.Poster.CroppedPosterURL, "/api/v1/temp/posters/test-job/TEST-002.jpg")
}

func TestScrapePosterGenerator_DownloadError_Sanitized(t *testing.T) {
	pm := NewPosterManager(afero.NewMemMapFs(), "/tmp", &genFailingHTTPClient{})
	gen := NewScrapePosterGenerator(pm, "", "")

	movie := &models.Movie{ID: "TEST-003", Poster: models.PosterState{PosterURL: "http://example.com/poster.jpg"}}
	err := gen.GeneratePoster(context.Background(), "test-job", movie)
	assert.Error(t, err)

	var se *sanitizedError
	assert.True(t, errors.As(err, &se), "error should be a sanitizedError")
}

// TestScrapePosterGenerator_RefererPassthrough verifies that when an explicit
// referer is set, it is forwarded to DownloadFromURL. When referer is empty,
// DownloadFromURL auto-derives it from the URL (tested in manager_test.go).
func TestScrapePosterGenerator_RefererPassthrough(t *testing.T) {
	var capturedReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReferer = r.Header.Get("Referer")
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

	// Test with explicit referer.
	gen := NewScrapePosterGenerator(pm, "", "https://custom.referer.com/")
	movie := &models.Movie{ID: "TEST-REF", Poster: models.PosterState{PosterURL: srv.URL + "/poster.jpg"}}
	err := gen.GeneratePoster(context.Background(), "test-job", movie)
	require.NoError(t, err)
	assert.Equal(t, "https://custom.referer.com/", capturedReferer)

	// Test with empty referer — DownloadFromURL auto-derives from URL.
	gen = NewScrapePosterGenerator(pm, "", "")
	movie = &models.Movie{ID: "TEST-REF2", Poster: models.PosterState{PosterURL: srv.URL + "/poster.jpg"}}
	err = gen.GeneratePoster(context.Background(), "test-job", movie)
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/", capturedReferer)
}

func TestScrapePosterGenerator_SSRFCheckPropagation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })
	gen := NewScrapePosterGenerator(pm, "", "").WithSSRFCheck(func(_ string) error { return nil })

	movie := &models.Movie{ID: "TEST-004", Poster: models.PosterState{PosterURL: srv.URL + "/poster.jpg"}}
	err := gen.GeneratePoster(context.Background(), "test-job", movie)
	require.NoError(t, err)
}

type genFailingHTTPClient struct{}

func (f *genFailingHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("network failure")
}

func TestScrapePosterGenerator_InterfaceSatisfaction(t *testing.T) {
	var _ PosterGenerator = NewScrapePosterGenerator(nil, "", "")
}
