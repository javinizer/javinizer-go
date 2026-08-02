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
	"os"
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
type failingRemoveFs struct {
	afero.Fs
}

func (f *failingRemoveFs) Remove(string) error {
	return errors.New("injected remove failure")
}

type stubOverridePosterGen struct {
	calls     int
	jobID     string
	movieID   string
	posterURL string
	err       error
	// stampCroppedURL, when non-empty, mimics the real generator stamping the
	// refreshed preview URL onto movie.Poster.CroppedPosterURL, so fan-out
	// tests can prove the refreshed preview reaches every part.
	stampCroppedURL string
}

func (s *stubOverridePosterGen) GeneratePoster(_ context.Context, jobID string, movie *models.Movie) error {
	s.calls++
	s.jobID = jobID
	s.movieID = movie.ID
	s.posterURL = movie.Poster.PosterURL
	if s.stampCroppedURL != "" {
		movie.Poster.CroppedPosterURL = s.stampCroppedURL
	}
	return s.err
}

// stubOverrideSnapshotter additionally implements the rollback capability
// (SnapshotPosterAssets/RestorePosterAssets) and the cleanup capability
// (RemovePosterAssets) so tests can drive the snapshot, rollback, and
// cache-clearing branches without a filesystem.
type stubOverrideSnapshotter struct {
	stubOverridePosterGen
	snapErr      error
	restoreErr   error
	restoreCalls int
	removeErr    error
	removeCalls  int
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

func (s *stubOverrideSnapshotter) RemovePosterAssets(_, _ string) error {
	s.removeCalls++
	return s.removeErr
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
	// A persisted preview URL that predates the refresh/cleanup, so tests can
	// prove the refresh overwrites it and the cleanup clears it.
	movie.Poster.CroppedPosterURL = "stale-preview-url"
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
		ovrPoster     string                   // poster URL the chosen source contributes
		ovrCover      string                   // cover URL the chosen source contributes
		gen           *stubOverridePosterGen   // nil = no poster infrastructure wired
		snap          *stubOverrideSnapshotter // rollback/cleanup-capable generator (takes precedence)
		wantCalls     int
		wantGenPoster string // PosterURL the generator was asked to regenerate from
		wantErr       string
		wantRestores  int
		wantRemoves   int
	}{
		{"poster_url change regenerates the source", "poster_url", oldURL, oldCover, newURL, newCover, &stubOverridePosterGen{}, nil, 1, newURL, "", 0, 0},
		{"identical URL leaves the current source alone", "poster_url", newURL, oldCover, newURL, newCover, &stubOverridePosterGen{}, nil, 0, "", "", 0, 0},
		{"non-poster override never regenerates", "maker", oldURL, oldCover, newURL, newCover, &stubOverridePosterGen{}, nil, 0, "", "", 0, 0},
		{"no generator wired skips regeneration", "poster_url", oldURL, oldCover, newURL, newCover, nil, nil, 0, "", "", 0, 0},
		{"regeneration failure rejects the override", "poster_url", oldURL, oldCover, newURL, newCover, &stubOverridePosterGen{err: errors.New("download failed")}, nil, 1, newURL, "refresh poster after source change", 0, 0},
		{"cover_url with no poster regenerates from the cover", "cover_url", "", oldCover, newURL, newCover, &stubOverridePosterGen{}, nil, 1, "", "", 0, 0},
		{"cover_url behind an explicit poster leaves the cache alone", "cover_url", oldURL, oldCover, newURL, newCover, &stubOverridePosterGen{}, nil, 0, "", "", 0, 0},
		{"identical cover_url leaves the current source alone", "cover_url", "", newCover, newURL, newCover, &stubOverridePosterGen{}, nil, 0, "", "", 0, 0},
		{"both poster sources already empty is a no-op", "poster_url", "", "", "", "", &stubOverridePosterGen{}, nil, 0, "", "", 0, 0},
		{"snapshot failure rejects the override before regenerating", "poster_url", oldURL, oldCover, newURL, newCover, nil, &stubOverrideSnapshotter{snapErr: errors.New("fs gone")}, 0, "", "snapshot poster before source change", 0, 0},
		{"regeneration failure rolls back the snapshot", "poster_url", oldURL, oldCover, newURL, newCover, nil, &stubOverrideSnapshotter{stubOverridePosterGen: stubOverridePosterGen{err: errors.New("download failed")}}, 1, newURL, "refresh poster after source change", 1, 0},
		{"clearing the last poster source cleans up instead of regenerating", "poster_url", oldURL, "", "", "", nil, &stubOverrideSnapshotter{}, 0, "", "", 0, 1},
		{"clearing the cover behind no poster cleans up", "cover_url", "", oldCover, "", "", nil, &stubOverrideSnapshotter{}, 0, "", "", 0, 1},
		{"clearing with a remover-less generator still clears the source", "poster_url", oldURL, "", "", "", &stubOverridePosterGen{}, nil, 0, "", "", 0, 0},
		{"cleanup removal failure rejects the edit and rolls back", "poster_url", oldURL, "", "", "", nil, &stubOverrideSnapshotter{removeErr: errors.New("fs remove gone")}, 0, "", "clear poster cache after source removal", 1, 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			je, tracker, resultID := overrideRefreshFixture(t, tt.currentPoster, tt.currentCover, tt.ovrPoster, tt.ovrCover)
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

			updated, _, _, err := je.ApplyFieldOverride(context.Background(), resultID, tt.field, "dmm")

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
					assert.Equal(t, tt.ovrPoster, updated.Movie.Poster.PosterURL)
				case "cover_url":
					assert.Equal(t, tt.ovrCover, updated.Movie.Poster.CoverURL)
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
				assert.Equal(t, tt.wantRemoves, snap.removeCalls)
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
		wantRemoved       bool   // whether the cached assets are expected to be cleaned up
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
			wantErr:           "refresh poster after source change",
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
		{
			name:             "clearing the last poster source removes the cached assets",
			field:            "poster_url",
			currentPosterURL: srv.URL + "/old.jpg",
			wantRemoved:      true, // no regeneration: an empty source must not keep a stale crop source
		},
		{
			name:            "clearing the cover behind no poster removes the cached assets",
			field:           "cover_url",
			currentCoverURL: srv.URL + "/old.jpg",
			wantRemoved:     true,
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
			previewPath := filepath.Join(tempPosterDir, "ABC-001.jpg")
			require.NoError(t, afero.WriteFile(fs, fullPath, oldJPEG, 0o644))
			if tt.wantRemoved {
				require.NoError(t, afero.WriteFile(fs, previewPath, oldJPEG, 0o644))
			}

			updated, _, _, err := je.ApplyFieldOverride(context.Background(), resultID, tt.field, "dmm")
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
				if tt.wantRemoved {
					assert.Empty(t, updated.Movie.Poster.CroppedPosterURL,
						"clearing the source must clear the preview URL from the persisted state")
				}
			}

			if tt.wantRemoved {
				for _, p := range []string{fullPath, previewPath} {
					_, statErr := fs.Stat(p)
					assert.True(t, os.IsNotExist(statErr), "%s must be removed when the last source is cleared", p)
				}
				return
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

	updated, _, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
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

func TestApplyFieldOverride_RollbackFailureReportedWithNoPreexistingAssets(t *testing.T) {
	jpeg := encodeTestJPEG(t, 200, 300, color.RGBA{B: 0xaa, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpeg)
	}))
	defer srv.Close()

	fs := &failingRemoveFs{Fs: afero.NewMemMapFs()}
	pm := poster.NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })
	je, _, resultID := overrideRefreshFixture(t, "old-poster", "", srv.URL, "")
	je.posterGen = poster.NewScrapePosterGenerator(pm, "", "")
	repo := mocks.NewMockMovieRepositoryInterface(t)
	repo.On("Upsert", mock.Anything, mock.Anything).Return(nil, errors.New("db down"))
	je.movieRepo = repo

	_, _, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist field override")
	assert.Contains(t, err.Error(), "poster rollback failed")
	assert.Contains(t, err.Error(), "injected remove failure")
}

// TestApplyFieldOverride_CleanupRollsBackWhenPersistFails is the cleanup twin
// of TestApplyFieldOverride_RefreshRollsBackWhenPersistFails: clearing the
// last poster source removes the cached assets, and when the following
// UpdateMovie fails the snapshot rollback must restore BOTH the removed
// -full.jpg/preview bytes AND leave the job's stored movie untouched (old
// source URL, crop bounds, and preview URL).
func TestApplyFieldOverride_CleanupRollsBackWhenPersistFails(t *testing.T) {
	oldJPEG := encodeTestJPEG(t, 200, 300, color.RGBA{R: 0xcc, A: 0xff})
	oldPreview := encodeTestJPEG(t, 50, 75, color.RGBA{G: 0x7f, A: 0xff})

	fs := afero.NewMemMapFs()
	pm := poster.NewPosterManager(fs, "/tmp", http.DefaultClient).WithSSRFCheck(func(_ string) error { return nil })
	gen := poster.NewScrapePosterGenerator(pm, "", "")
	je, tracker, resultID := overrideRefreshFixture(t, "https://old.example/poster.jpg", "", "", "")
	je.posterGen = gen

	// Persistence fails AFTER the cleanup removed the cached assets.
	repo := mocks.NewMockMovieRepositoryInterface(t)
	repo.On("Upsert", mock.Anything, mock.Anything).Return(nil, errors.New("db down"))
	je.movieRepo = repo

	tempPosterDir := filepath.Join("/tmp", "posters", "job1")
	require.NoError(t, fs.MkdirAll(tempPosterDir, 0o755))
	fullPath := filepath.Join(tempPosterDir, "ABC-001-full.jpg")
	previewPath := filepath.Join(tempPosterDir, "ABC-001.jpg")
	require.NoError(t, afero.WriteFile(fs, fullPath, oldJPEG, 0o644))
	require.NoError(t, afero.WriteFile(fs, previewPath, oldPreview, 0o644))

	updated, _, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist field override")
	assert.Nil(t, updated)

	// Filesystem rolled back to the pre-cleanup bytes.
	full, readErr := afero.ReadFile(fs, fullPath)
	require.NoError(t, readErr)
	assert.True(t, bytes.Equal(oldJPEG, full),
		"-full.jpg must be restored when persistence fails, got %x want %x", full, oldJPEG)
	preview, readErr := afero.ReadFile(fs, previewPath)
	require.NoError(t, readErr)
	assert.True(t, bytes.Equal(oldPreview, preview),
		"preview must be restored when persistence fails, got %x want %x", preview, oldPreview)

	// Job state intact: the clearing override was never persisted.
	current, _, found := tracker.GetFileResultByResultID(resultID)
	require.True(t, found)
	assert.Equal(t, "https://old.example/poster.jpg", current.Movie.Poster.PosterURL)
	assert.Equal(t, "", current.Movie.Poster.CoverURL)
	assert.Equal(t, "stale-preview-url", current.Movie.Poster.CroppedPosterURL)
	require.NotNil(t, current.Movie.Poster.CropBounds,
		"a failed override must not discard the still-valid crop bounds")
}

// TestRefreshPosterAssets_CleanupRemoveAndRestoreFailures ensures a failed
// asset restore inside the cleanup-error branch is not swallowed: when the
// cache removal fails and the snapshot rollback also fails, both errors must
// surface (same join style as the regeneration-error branch) so the resulting
// cache/job-state mismatch is observable.
func TestRefreshPosterAssets_CleanupRemoveAndRestoreFailures(t *testing.T) {
	removeErr := errors.New("remove failed")
	restoreErr := errors.New("restore failed")
	gen := &stubOverrideSnapshotter{removeErr: removeErr, restoreErr: restoreErr}
	movie := &models.Movie{ID: "ABC-001", Poster: models.PosterState{CroppedPosterURL: "stale-preview-url"}}

	rollback, err := RefreshPosterAssets(context.Background(), gen, "job1", movie, "https://old.example/poster.jpg", "")

	require.Error(t, err)
	assert.Nil(t, rollback)
	assert.ErrorIs(t, err, removeErr)
	assert.ErrorIs(t, err, restoreErr)
	assert.Contains(t, err.Error(), "clear poster cache after source removal")
	assert.Contains(t, err.Error(), "poster rollback failed")
	assert.Equal(t, 1, gen.removeCalls)
	assert.Equal(t, 1, gen.restoreCalls)
	assert.Empty(t, movie.Poster.CroppedPosterURL, "the preview URL is cleared as part of the cleanup attempt")
}

// TestRefreshOverriddenPosterSource_GenerateAndRestoreFailures ensures a
// failed asset restore inside the generation-error branch is not swallowed:
// when GeneratePoster fails after replacing or removing cached assets, the
// rollback is the only attempt to restore them, so its failure must surface
// alongside the refresh error (same join style as the persist-failure
// branch). Otherwise the cache could be absent/mismatched while only the
// download error is reported.
func TestRefreshOverriddenPosterSource_GenerateAndRestoreFailures(t *testing.T) {
	generateErr := errors.New("generate failed")
	restoreErr := errors.New("restore failed")
	gen := &stubOverrideSnapshotter{
		stubOverridePosterGen: stubOverridePosterGen{err: generateErr},
		restoreErr:            restoreErr,
	}
	je, _, _ := overrideRefreshFixture(t, "https://old.example/poster.jpg", "", "dmm-poster-url", "")
	je.posterGen = gen
	movie := &models.Movie{ID: "ABC-001", Poster: models.PosterState{PosterURL: "dmm-poster-url"}}

	_, err := je.refreshOverriddenPosterSource(context.Background(), movie, "https://old.example/poster.jpg", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, generateErr)
	assert.ErrorIs(t, err, restoreErr)
	assert.Contains(t, err.Error(), "refresh poster after source change")
	assert.Contains(t, err.Error(), "generate failed")
	assert.Contains(t, err.Error(), "poster rollback failed")
	assert.Contains(t, err.Error(), "restore failed")
	assert.Equal(t, 1, gen.restoreCalls)
}

func TestApplyFieldOverride_RollbackFailureReported(t *testing.T) {
	gen := &stubOverrideSnapshotter{restoreErr: errors.New("restore broke")}
	je, _, resultID := overrideRefreshFixture(t, "https://old.example/poster.jpg", "", "dmm-poster-url", "")
	je.posterGen = gen
	repo := mocks.NewMockMovieRepositoryInterface(t)
	repo.On("Upsert", mock.Anything, mock.Anything).Return(nil, errors.New("db down"))
	je.movieRepo = repo

	_, _, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
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

// TestApplyFieldOverride_CoverBackedSameEffectiveSourceSkipsRefreshAndKeepsCrop
// pins the FULL override-path behavior for the cover-backed U→U case (Codex:
// "preserve crops when the effective source is unchanged"): with PosterURL ==
// "" and CoverURL == U, selecting a source whose PosterURL is also U must not
// regenerate the poster cache (RefreshPosterAssets already no-ops on the
// effective source), must not clear the approved manual crop, and must not
// re-derive the crop intent — otherwise the review preview stays cropped
// while Organize discards the approved bounds for the scraper's intent.
func TestApplyFieldOverride_CoverBackedSameEffectiveSourceSkipsRefreshAndKeepsCrop(t *testing.T) {
	const coverU = "https://shared.example/cover.jpg"
	je, tracker, resultID := overrideRefreshFixture(t, "", coverU, coverU, "dmm-cover-url")
	gen := &stubOverridePosterGen{}
	je.posterGen = gen
	// A manual crop approved against the cover: the crop endpoint flipped
	// ShouldCropPoster=false and recorded SourceWasCover=true.
	require.NoError(t, tracker.AtomicUpdateFileResult("test.mp4", func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		current.Movie.Poster.ShouldCropPoster = false
		current.Movie.Poster.CropBounds.SourceWasCover = true
		return current, nil
	}))

	updated, _, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, 0, gen.calls,
		"unchanged effective source: the poster cache refresh must no-op (parity with RefreshPosterAssets)")
	assert.Equal(t, coverU, updated.Movie.Poster.PosterURL)
	require.NotNil(t, updated.Movie.Poster.CropBounds,
		"the approved manual crop must survive a raw PosterURL change that leaves the effective source untouched")
	assert.True(t, updated.Movie.Poster.CropBounds.SourceWasCover)
	assert.False(t, updated.Movie.Poster.ShouldCropPoster)

	final, _, found := tracker.GetFileResultByResultID(resultID)
	require.True(t, found)
	require.NotNil(t, final.Movie.Poster.CropBounds,
		"the preserved bounds must survive the persist round-trip")
	assert.True(t, final.Movie.Poster.CropBounds.SourceWasCover)
}

// TestApplyFieldOverride_CoverBackedTruePosterChangeRefreshesAndClearsCrop is
// the companion guard at the override-path level: a cover-backed movie that
// picks a DIFFERENT poster URL really changed the effective source, so the
// cache refresh runs and the old cover-measured crop is invalidated.
func TestApplyFieldOverride_CoverBackedTruePosterChangeRefreshesAndClearsCrop(t *testing.T) {
	const coverU = "https://shared.example/cover.jpg"
	const newPoster = "https://new.example/poster.jpg"
	je, _, resultID := overrideRefreshFixture(t, "", coverU, newPoster, "")
	gen := &stubOverridePosterGen{}
	je.posterGen = gen

	updated, _, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, newPoster, updated.Movie.Poster.PosterURL)
	assert.Equal(t, 1, gen.calls,
		"the effective source really changed (cover → poster): the refresh must regenerate")
	assert.Nil(t, updated.Movie.Poster.CropBounds,
		"a crop measured against the cover cannot survive onto a different image")
	assert.True(t, updated.Movie.Poster.ShouldCropPoster,
		"the selected source's own crop intent travels with its image")
}
