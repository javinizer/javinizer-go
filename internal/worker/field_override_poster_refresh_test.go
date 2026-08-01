package worker

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubOverridePosterGen records GeneratePoster invocations so tests can
// observe the refresh decision without touching a filesystem.
type stubOverridePosterGen struct {
	calls     int
	jobID     string
	movieID   string
	posterURL string
	err       error
}

func (s *stubOverridePosterGen) GeneratePoster(_ context.Context, jobID string, movie *models.Movie) error {
	s.calls++
	s.jobID = jobID
	s.movieID = movie.ID
	s.posterURL = movie.Poster.PosterURL
	return s.err
}

// overrideRefreshFixture builds a jobEditorImpl over a single completed result
// whose movie carries currentPosterURL and a crop measured against it, plus
// provenance whose "dmm" raw result carries overrideSourceURL. The caller wires
// je.posterGen itself (a typed nil generator would hide behind a non-nil
// interface and bypass the nil-generator skip).
func overrideRefreshFixture(t *testing.T, currentPosterURL, overrideSourceURL string) (*jobEditorImpl, resultstore.Store, string) {
	t.Helper()
	movie, prov := overrideFixture()
	movie.ID = "ABC-001"
	movie.Poster.PosterURL = currentPosterURL
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 100, Height: 200}
	findScraperResult(prov.ScraperResults, "dmm").PosterURL = overrideSourceURL

	filePath := "test.mp4"
	resultID := "res-001"
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID:      resultID,
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movie.ID},
		Movie:         movie,
		Status:        models.JobStatusCompleted,
	})
	tracker.SetProvenance(filePath, prov)

	return &jobEditorImpl{store: tracker, jobID: "job1"}, tracker, resultID
}

// TestApplyFieldOverride_PosterURLRefreshInvocation pins WHEN the temp
// full-size source is refreshed: only a poster_url override that actually
// changes the URL regenerates it. The review client treats the persisted
// poster_url as "already synced" and skips its own poster-from-url call, so a
// missed refresh would let a manual crop measure the stale pre-override
// -full.jpg.
func TestApplyFieldOverride_PosterURLRefreshInvocation(t *testing.T) {
	const (
		oldURL = "https://old.example/poster.jpg"
		newURL = "dmm-poster-url" // overrideFixture's dmm source URL
	)
	cases := []struct {
		name       string
		field      string
		currentURL string
		gen        *stubOverridePosterGen // nil = no poster infrastructure wired
		wantCalls  int
		wantErr    string
	}{
		{"poster_url change regenerates the source", "poster_url", oldURL, &stubOverridePosterGen{}, 1, ""},
		{"identical URL leaves the current source alone", "poster_url", newURL, &stubOverridePosterGen{}, 0, ""},
		{"non-poster override never regenerates", "maker", oldURL, &stubOverridePosterGen{}, 0, ""},
		{"no generator wired skips regeneration", "poster_url", oldURL, nil, 0, ""},
		{"regeneration failure rejects the override", "poster_url", oldURL, &stubOverridePosterGen{err: errors.New("download failed")}, 1, "refresh poster after field override"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			je, tracker, resultID := overrideRefreshFixture(t, tt.currentURL, newURL)
			if tt.gen != nil {
				je.posterGen = tt.gen
			}

			updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, tt.field, "dmm")

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				// The failed refresh must roll the override back with it:
				// persisting the new URL against the stale on-disk image is
				// exactly the desync this fix exists to prevent.
				current, _, found := tracker.GetFileResultByResultID(resultID)
				require.True(t, found)
				assert.Equal(t, tt.currentURL, current.Movie.Poster.PosterURL)
				require.NotNil(t, current.Movie.Poster.CropBounds,
					"a rejected override must not discard the still-valid crop bounds")
			} else {
				require.NoError(t, err)
				require.NotNil(t, updated)
				if tt.field == "poster_url" {
					assert.Equal(t, newURL, updated.Movie.Poster.PosterURL)
				}
			}

			if tt.gen != nil {
				assert.Equal(t, tt.wantCalls, tt.gen.calls)
				if tt.wantCalls > 0 {
					assert.Equal(t, "job1", tt.gen.jobID)
					assert.Equal(t, "ABC-001", tt.gen.movieID)
					assert.Equal(t, newURL, tt.gen.posterURL,
						"the refresh must regenerate from the overridden URL, not the old one")
				}
			}
		})
	}
}

// TestApplyFieldOverride_PosterURLRefreshTempFiles exercises the refresh
// through the real PosterManager + httptest download path, verifying the
// observable the fix promises: {jobID}/{movie.ID}-full.jpg on disk tracks the
// overridden poster URL, and a failed download never clobbers the cached
// full-size image (PosterManager writes via temp file before replacing).
func TestApplyFieldOverride_PosterURLRefreshTempFiles(t *testing.T) {
	oldJPEG := encodeTestJPEG(t, 200, 300, color.RGBA{R: 0xcc, A: 0xff})
	newJPEG := encodeTestJPEG(t, 220, 330, color.RGBA{B: 0xaa, A: 0xff})
	require.NotEqual(t, oldJPEG, newJPEG, "fixture images must be distinguishable")

	var newHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/old.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(oldJPEG)
	})
	mux.HandleFunc("/new.jpg", func(w http.ResponseWriter, _ *http.Request) {
		newHits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(newJPEG)
	})
	mux.HandleFunc("/broken.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cases := []struct {
		name           string
		currentURL     string
		overrideURL    string
		wantErr        string
		wantFullSource []byte // expected -full.jpg contents after the call
	}{
		{
			name:           "changed URL rewrites the full-size source",
			currentURL:     srv.URL + "/old.jpg",
			overrideURL:    srv.URL + "/new.jpg",
			wantFullSource: newJPEG,
		},
		{
			name:           "identical URL keeps the cached file untouched",
			currentURL:     srv.URL + "/new.jpg",
			overrideURL:    srv.URL + "/new.jpg",
			wantFullSource: oldJPEG, // seeded; never re-downloaded
		},
		{
			name:           "failed download rejects the override and preserves the cached file",
			currentURL:     srv.URL + "/old.jpg",
			overrideURL:    srv.URL + "/broken.jpg",
			wantErr:        "refresh poster after field override",
			wantFullSource: oldJPEG, // new image never replaces a good cache entry
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			pm := poster.NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })
			gen := poster.NewScrapePosterGenerator(pm, "", "")
			je, tracker, resultID := overrideRefreshFixture(t, tt.currentURL, tt.overrideURL)
			je.posterGen = gen

			// Seed the scraped full-size source the crop endpoint would
			// otherwise hit, so the refresh's effect is observable.
			tempPosterDir := filepath.Join("/tmp", "posters", "job1")
			require.NoError(t, fs.MkdirAll(tempPosterDir, 0o755))
			fullPath := filepath.Join(tempPosterDir, "ABC-001-full.jpg")
			require.NoError(t, afero.WriteFile(fs, fullPath, oldJPEG, 0o644))

			updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				// The failed refresh rolls the model change back with it.
				current, _, found := tracker.GetFileResultByResultID(resultID)
				require.True(t, found)
				assert.Equal(t, tt.currentURL, current.Movie.Poster.PosterURL)
			} else {
				require.NoError(t, err)
				require.NotNil(t, updated)
				assert.Equal(t, tt.overrideURL, updated.Movie.Poster.PosterURL)
				if tt.currentURL != tt.overrideURL {
					// The refresh also repoints the persisted temp preview at the
					// newly generated poster, so the review page shows the new image.
					assert.Contains(t, updated.Movie.Poster.CroppedPosterURL,
						"/api/v1/temp/posters/job1/ABC-001.jpg")
				}
			}

			content, readErr := afero.ReadFile(fs, fullPath)
			require.NoError(t, readErr)
			assert.True(t, bytes.Equal(tt.wantFullSource, content),
				"-full.jpg contents after override = %x, want %x", content, tt.wantFullSource)
		})
	}

	// Only the changed-URL case may have fetched the new image.
	assert.Equal(t, 1, newHits, "/new.jpg must be fetched exactly once (changed-URL case only)")
}

// encodeTestJPEG renders a solid-color JPEG of the given size.
func encodeTestJPEG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}))
	return buf.Bytes()
}
