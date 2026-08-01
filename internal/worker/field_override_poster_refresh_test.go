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

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

// stubOverrideSnapshotter additionally implements the rollback capability
// (SnapshotPosterAssets/RestorePosterAssets) so tests can drive the
// snapshot and rollback branches without a filesystem.
type stubOverrideSnapshotter struct {
	stubOverridePosterGen
	snapErr      error
	restoreErr   error
	restoreCalls int
}

func (s *stubOverrideSnapshotter) SnapshotPosterAssets(_, _ string) (*poster.AssetsSnapshot, error) {
	if s.snapErr != nil {
		return nil, s.snapErr
	}
	return &poster.AssetsSnapshot{}, nil
}

func (s *stubOverrideSnapshotter) RestorePosterAssets(_ *poster.AssetsSnapshot) error {
	s.restoreCalls++
	return s.restoreErr
}

// overrideRefreshFixture builds a jobEditorImpl over a single completed result
// whose movie carries the given current poster/cover URLs and a crop measured
// against them, plus provenance whose "dmm" raw result carries
// overridePosterURL/overrideCoverURL. The caller wires je.posterGen itself (a
// typed nil generator would hide behind a non-nil interface and bypass the
// nil-generator skip).
func overrideRefreshFixture(t *testing.T, currentPosterURL, currentCoverURL, overridePosterURL, overrideCoverURL string) (*jobEditorImpl, resultstore.Store, string) {
	t.Helper()
	movie, prov := overrideFixture()
	movie.ID = "ABC-001"
	movie.Poster.PosterURL = currentPosterURL
	movie.Poster.CoverURL = currentCoverURL
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 100, Height: 200}
	dmm := findScraperResult(prov.ScraperResults, "dmm")
	dmm.PosterURL = overridePosterURL
	dmm.CoverURL = overrideCoverURL

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
// full-size source is refreshed: only a poster_url override — or a cover_url
// override whose cover is the effective poster source (PosterURL empty) — that
// actually changes that source regenerates it. The review client treats the
// persisted URL as "already synced" and skips its own poster-from-url call, so
// a missed refresh would let a manual crop measure the stale pre-override
// -full.jpg.
func TestApplyFieldOverride_PosterURLRefreshInvocation(t *testing.T) {
	const (
		oldURL   = "https://old.example/poster.jpg"
		oldCover = "https://old.example/cover.jpg"
		newURL   = "dmm-poster-url" // overrideFixture's dmm source URLs
		newCover = "dmm-cover-url"
	)
	cases := []struct {
		name          string
		field         string
		currentPoster string
		currentCover  string
		gen           *stubOverridePosterGen   // nil = no poster infrastructure wired
		snap          *stubOverrideSnapshotter // rollback-capable generator (takes precedence)
		wantCalls     int
		wantGenPoster string // PosterURL the generator was asked to regenerate from
		wantErr       string
		wantRestores  int
	}{
		{"poster_url change regenerates the source", "poster_url", oldURL, oldCover, &stubOverridePosterGen{}, nil, 1, newURL, "", 0},
		{"identical URL leaves the current source alone", "poster_url", newURL, oldCover, &stubOverridePosterGen{}, nil, 0, "", "", 0},
		{"non-poster override never regenerates", "maker", oldURL, oldCover, &stubOverridePosterGen{}, nil, 0, "", "", 0},
		{"no generator wired skips regeneration", "poster_url", oldURL, oldCover, nil, nil, 0, "", "", 0},
		{"regeneration failure rejects the override", "poster_url", oldURL, oldCover, &stubOverridePosterGen{err: errors.New("download failed")}, nil, 1, newURL, "refresh poster after field override", 0},
		{"cover_url with no poster regenerates from the cover", "cover_url", "", oldCover, &stubOverridePosterGen{}, nil, 1, "", "", 0},
		{"cover_url behind an explicit poster leaves the cache alone", "cover_url", oldURL, oldCover, &stubOverridePosterGen{}, nil, 0, "", "", 0},
		{"identical cover_url leaves the current source alone", "cover_url", "", newCover, &stubOverridePosterGen{}, nil, 0, "", "", 0},
		{"snapshot failure rejects the override before regenerating", "poster_url", oldURL, oldCover, nil, &stubOverrideSnapshotter{snapErr: errors.New("fs gone")}, 0, "", "snapshot poster before field override", 0},
		{"regeneration failure rolls back the snapshot", "poster_url", oldURL, oldCover, nil, &stubOverrideSnapshotter{stubOverridePosterGen: stubOverridePosterGen{err: errors.New("download failed")}}, 1, newURL, "refresh poster after field override", 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			je, tracker, resultID := overrideRefreshFixture(t, tt.currentPoster, tt.currentCover, newURL, newCover)
			var counter *stubOverridePosterGen
			var snap *stubOverrideSnapshotter
			if tt.snap != nil {
				je.posterGen = tt.snap
				counter = &tt.snap.stubOverridePosterGen
				snap = tt.snap
			} else if tt.gen != nil {
				je.posterGen = tt.gen
				counter = tt.gen
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
				assert.Equal(t, tt.currentPoster, current.Movie.Poster.PosterURL)
				assert.Equal(t, tt.currentCover, current.Movie.Poster.CoverURL)
				require.NotNil(t, current.Movie.Poster.CropBounds,
					"a rejected override must not discard the still-valid crop bounds")
			} else {
				require.NoError(t, err)
				require.NotNil(t, updated)
				switch tt.field {
				case "poster_url":
					assert.Equal(t, newURL, updated.Movie.Poster.PosterURL)
				case "cover_url":
					assert.Equal(t, newCover, updated.Movie.Poster.CoverURL)
				}
			}

			if counter != nil {
				assert.Equal(t, tt.wantCalls, counter.calls)
				if tt.wantCalls > 0 {
					assert.Equal(t, "job1", counter.jobID)
					assert.Equal(t, "ABC-001", counter.movieID)
					assert.Equal(t, tt.wantGenPoster, counter.posterURL,
						"the refresh must regenerate from the overridden source, not the old one")
				}
			}
			if snap != nil {
				assert.Equal(t, tt.wantRestores, snap.restoreCalls)
			}
		})
	}
}

// TestApplyFieldOverride_PosterURLRefreshTempFiles exercises the refresh
// through the real PosterManager + httptest download path, verifying the
// observable the fix promises: {jobID}/{movie.ID}-full.jpg on disk tracks the
// overridden poster source (the poster URL, or the cover URL when no poster is
// set), and a failed download never clobbers the cached full-size image
// (PosterManager writes via temp file before replacing).
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
		name              string
		field             string
		currentPosterURL  string
		currentCoverURL   string
		overridePosterURL string
		overrideCoverURL  string
		wantErr           string
		wantRefresh       bool   // whether -full.jpg is expected to be regenerated
		wantFullSource    []byte // expected -full.jpg contents after the call
	}{
		{
			name:              "changed poster URL rewrites the full-size source",
			field:             "poster_url",
			currentPosterURL:  srv.URL + "/old.jpg",
			overridePosterURL: srv.URL + "/new.jpg",
			wantRefresh:       true,
			wantFullSource:    newJPEG,
		},
		{
			name:              "identical poster URL keeps the cached file untouched",
			field:             "poster_url",
			currentPosterURL:  srv.URL + "/new.jpg",
			overridePosterURL: srv.URL + "/new.jpg",
			wantFullSource:    oldJPEG, // seeded; never re-downloaded
		},
		{
			name:              "failed poster download rejects the override and preserves the cached file",
			field:             "poster_url",
			currentPosterURL:  srv.URL + "/old.jpg",
			overridePosterURL: srv.URL + "/broken.jpg",
			wantErr:           "refresh poster after field override",
			wantFullSource:    oldJPEG, // new image never replaces a good cache entry
		},
		{
			name:             "cover override without a poster regenerates from the new cover",
			field:            "cover_url",
			currentCoverURL:  srv.URL + "/old.jpg",
			overrideCoverURL: srv.URL + "/new.jpg",
			wantRefresh:      true,
			wantFullSource:   newJPEG, // downloader falls back to CoverURL when PosterURL is empty
		},
		{
			name:              "cover override behind an explicit poster leaves the cache untouched",
			field:             "cover_url",
			currentPosterURL:  srv.URL + "/old.jpg",
			currentCoverURL:   srv.URL + "/old.jpg",
			overridePosterURL: "", // dmm raw record's poster URL is irrelevant here
			overrideCoverURL:  srv.URL + "/new.jpg",
			wantFullSource:    oldJPEG, // PosterURL is the effective source; unchanged
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			pm := poster.NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })
			gen := poster.NewScrapePosterGenerator(pm, "", "")
			je, tracker, resultID := overrideRefreshFixture(t, tt.currentPosterURL, tt.currentCoverURL, tt.overridePosterURL, tt.overrideCoverURL)
			je.posterGen = gen

			// Seed the scraped full-size source the crop endpoint would
			// otherwise hit, so the refresh's effect is observable.
			tempPosterDir := filepath.Join("/tmp", "posters", "job1")
			require.NoError(t, fs.MkdirAll(tempPosterDir, 0o755))
			fullPath := filepath.Join(tempPosterDir, "ABC-001-full.jpg")
			require.NoError(t, afero.WriteFile(fs, fullPath, oldJPEG, 0o644))

			updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, tt.field, "dmm")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				// The failed refresh rolls the model change back with it.
				current, _, found := tracker.GetFileResultByResultID(resultID)
				require.True(t, found)
				assert.Equal(t, tt.currentPosterURL, current.Movie.Poster.PosterURL)
				assert.Equal(t, tt.currentCoverURL, current.Movie.Poster.CoverURL)
			} else {
				require.NoError(t, err)
				require.NotNil(t, updated)
				switch tt.field {
				case "poster_url":
					assert.Equal(t, tt.overridePosterURL, updated.Movie.Poster.PosterURL)
				case "cover_url":
					assert.Equal(t, tt.overrideCoverURL, updated.Movie.Poster.CoverURL)
				}
				if tt.wantRefresh {
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

	// Only the two changed-URL cases may have fetched the new image.
	assert.Equal(t, 2, newHits, "/new.jpg must be fetched once per refresh (poster + cover-backed cases)")
}

// TestApplyFieldOverride_RefreshRollsBackWhenPersistFails guards the filesystem/
// job-state invariant: if GeneratePoster succeeds but the following UpdateMovie
// fails, the cached -full.jpg and preview are restored to the pre-refresh bytes
// so a subsequent crop does not measure the new image against the job's old
// source URL.
func TestApplyFieldOverride_RefreshRollsBackWhenPersistFails(t *testing.T) {
	oldJPEG := encodeTestJPEG(t, 200, 300, color.RGBA{R: 0xcc, A: 0xff})
	oldPreview := encodeTestJPEG(t, 50, 75, color.RGBA{G: 0x7f, A: 0xff})

	var newHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/old.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(oldJPEG)
	})
	mux.HandleFunc("/new.jpg", func(w http.ResponseWriter, _ *http.Request) {
		newHits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(encodeTestJPEG(t, 220, 330, color.RGBA{B: 0xaa, A: 0xff}))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	fs := afero.NewMemMapFs()
	pm := poster.NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })
	gen := poster.NewScrapePosterGenerator(pm, "", "")
	je, tracker, resultID := overrideRefreshFixture(t, srv.URL+"/old.jpg", "", srv.URL+"/new.jpg", "")
	je.posterGen = gen

	// Persistence fails AFTER the refresh swapped the cached assets.
	repo := mocks.NewMockMovieRepositoryInterface(t)
	repo.On("Upsert", mock.Anything, mock.Anything).Return(nil, errors.New("db down"))
	je.movieRepo = repo

	tempPosterDir := filepath.Join("/tmp", "posters", "job1")
	require.NoError(t, fs.MkdirAll(tempPosterDir, 0o755))
	fullPath := filepath.Join(tempPosterDir, "ABC-001-full.jpg")
	previewPath := filepath.Join(tempPosterDir, "ABC-001.jpg")
	require.NoError(t, afero.WriteFile(fs, fullPath, oldJPEG, 0o644))
	require.NoError(t, afero.WriteFile(fs, previewPath, oldPreview, 0o644))

	updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist field override")
	assert.Nil(t, updated)
	assert.Equal(t, 1, newHits, "sanity: the refresh really downloaded the new image before persistence failed")

	// Filesystem rolled back to the pre-refresh bytes.
	full, readErr := afero.ReadFile(fs, fullPath)
	require.NoError(t, readErr)
	assert.True(t, bytes.Equal(oldJPEG, full),
		"-full.jpg must be restored when persistence fails, got %x want %x", full, oldJPEG)
	preview, readErr := afero.ReadFile(fs, previewPath)
	require.NoError(t, readErr)
	assert.True(t, bytes.Equal(oldPreview, preview),
		"preview must be restored when persistence fails, got %x want %x", preview, oldPreview)

	// Job state intact: the override was never persisted.
	current, _, found := tracker.GetFileResultByResultID(resultID)
	require.True(t, found)
	assert.Equal(t, srv.URL+"/old.jpg", current.Movie.Poster.PosterURL)
	assert.Equal(t, "", current.Movie.Poster.CoverURL)
	require.NotNil(t, current.Movie.Poster.CropBounds,
		"a failed override must not discard the still-valid crop bounds")
}

// TestApplyFieldOverride_RollbackFailureReported ensures a failed asset
// restore is not swallowed: the persist error is still primary, and the
// rollback failure is surfaced alongside it so the filesystem/job-state desync
// is observable.
func TestApplyFieldOverride_RollbackFailureReported(t *testing.T) {
	gen := &stubOverrideSnapshotter{restoreErr: errors.New("restore broke")}
	je, _, resultID := overrideRefreshFixture(t, "https://old.example/poster.jpg", "", "dmm-poster-url", "")
	je.posterGen = gen
	repo := mocks.NewMockMovieRepositoryInterface(t)
	repo.On("Upsert", mock.Anything, mock.Anything).Return(nil, errors.New("db down"))
	je.movieRepo = repo

	_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist field override")
	assert.Contains(t, err.Error(), "poster rollback failed")
	assert.Equal(t, 1, gen.restoreCalls)
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
