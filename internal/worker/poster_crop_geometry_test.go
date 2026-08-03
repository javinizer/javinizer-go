package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cropGeometryFixture() *models.CropBounds {
	return &models.CropBounds{X: 0.1, Y: 0.05, Width: 0.4, Height: 0.9, SourceAspect: 1.667}
}

func newCropGeometryJob(t *testing.T) (*BatchJob, string) {
	t.Helper()
	const filePath = "/tmp/CROPGEO-001.mp4"
	jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
	job := jq.CreateJobBatch([]string{filePath})
	job.results.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "CROPGEO-001"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:    "CROPGEO-001",
			Title: "Subject",
			Poster: models.PosterState{
				PosterURL:        "https://cdn.example/poster.jpg",
				CoverURL:         "https://cdn.example/cover.jpg",
				ShouldCropPoster: true,
			},
		},
		StartedAt: time.Now(),
	})
	return job, filePath
}

func storedPosterState(t *testing.T, job *BatchJob, filePath string) models.PosterState {
	t.Helper()
	res, err := job.results.GetMovieResult(filePath)
	require.NoError(t, err)
	require.NotNil(t, res.Movie)
	return res.Movie.Poster
}

// The crop endpoint's editor call stores normalized geometry next to the
// cropped preview URL for every file of the movie.
// Nil-receiver guards on the geometry helpers: defensive branches must hold
// without touching state.
func TestCropGeometryHelpers_NilGuards(t *testing.T) {
	t.Parallel()
	clearPosterCropGeometry(nil)                           // no panic
	sanitizePosterCropGeometry(nil, true, "a", "b", false) // no panic
}
func TestUpdatePosterCrop_StoresGeometry(t *testing.T) {
	job, filePath := newCropGeometryJob(t)

	err := job.posterEditor.UpdatePosterCrop("CROPGEO-001", "/api/v1/temp/posters/job/CROPGEO-001.jpg?v=1", cropGeometryFixture(), true)
	require.NoError(t, err)

	poster := storedPosterState(t, job, filePath)
	require.NotNil(t, poster.PosterCropBounds)
	assert.Equal(t, *cropGeometryFixture(), *poster.PosterCropBounds)
	assert.True(t, poster.PosterCropSourceFull)
	assert.Equal(t, "/api/v1/temp/posters/job/CROPGEO-001.jpg?v=1", poster.CroppedPosterURL)
	assert.False(t, poster.ShouldCropPoster)
}

// A crop measured against the legacy already-cropped preview stores nothing
// applyable — and clears any previously stored geometry.
func TestUpdatePosterCrop_LegacyClearsGeometry(t *testing.T) {
	job, filePath := newCropGeometryJob(t)
	require.NoError(t, job.posterEditor.UpdatePosterCrop("CROPGEO-001", "/tmp/c1.jpg", cropGeometryFixture(), true))
	require.NotNil(t, storedPosterState(t, job, filePath).PosterCropBounds)

	require.NoError(t, job.posterEditor.UpdatePosterCrop("CROPGEO-001", "/tmp/c2.jpg", nil, false))

	poster := storedPosterState(t, job, filePath)
	assert.Nil(t, poster.PosterCropBounds)
	assert.False(t, poster.PosterCropSourceFull)
	assert.Equal(t, "/tmp/c2.jpg", poster.CroppedPosterURL)
}

// Poster-from-URL replaces the poster source: stored geometry is stale.
func TestUpdatePosterFromURL_ClearsGeometry(t *testing.T) {
	job, filePath := newCropGeometryJob(t)
	require.NoError(t, job.posterEditor.UpdatePosterCrop("CROPGEO-001", "/tmp/c1.jpg", cropGeometryFixture(), true))

	require.NoError(t, job.posterEditor.UpdatePosterFromURL(context.Background(), "CROPGEO-001",
		"https://cdn.example/new-poster.jpg", "/tmp/new-crop.jpg"))

	poster := storedPosterState(t, job, filePath)
	assert.Equal(t, "https://cdn.example/new-poster.jpg", poster.PosterURL)
	assert.Nil(t, poster.PosterCropBounds, "new poster source invalidates stored geometry")
	assert.False(t, poster.PosterCropSourceFull)
}

// Per-scraper field overrides that touch the poster source or crop intent
// clear stored geometry.
func TestApplyFieldOverridePosterKeys_ClearGeometry(t *testing.T) {
	for _, key := range []string{"poster_url", "cover_url", "should_crop_poster"} {
		t.Run(key, func(t *testing.T) {
			movie, prov := overrideFixture()
			movie.Poster.PosterCropBounds = cropGeometryFixture()
			movie.Poster.PosterCropSourceFull = true

			require.NoError(t, applyFieldOverride(movie, prov, key, "dmm"))
			assert.Nil(t, movie.Poster.PosterCropBounds, "%s override must clear geometry", key)
			assert.False(t, movie.Poster.PosterCropSourceFull)
		})
	}

	t.Run("cover_url with an explicit poster keeps geometry", func(t *testing.T) {
		movie, prov := overrideFixture()
		movie.Poster.PosterURL = "https://cdn.example/explicit-poster.jpg" // poster is the effective source
		movie.Poster.PosterCropBounds = cropGeometryFixture()
		movie.Poster.PosterCropSourceFull = true

		require.NoError(t, applyFieldOverride(movie, prov, "cover_url", "dmm"))
		require.NotNil(t, movie.Poster.PosterCropBounds, "fanart override under explicit poster must not discard the crop")
		assert.True(t, movie.Poster.PosterCropSourceFull)
	})
}

// sanitizePosterCropGeometry: stored-geometry contract on whole-movie saves.
func TestSanitizePosterCropGeometry(t *testing.T) {
	const (
		curPoster = "https://cdn.example/poster.jpg"
		curCover  = "https://cdn.example/cover.jpg"
	)
	mk := func() *models.Movie {
		m := &models.Movie{ID: "CROPGEO-001"}
		m.Poster.PosterURL = curPoster
		m.Poster.CoverURL = curCover
		m.Poster.PosterCropBounds = cropGeometryFixture()
		m.Poster.PosterCropSourceFull = true
		return m
	}

	t.Run("unchanged source and intent keeps geometry", func(t *testing.T) {
		next := mk()
		sanitizePosterCropGeometry(next, true, curPoster, curCover, false)
		require.NotNil(t, next.Poster.PosterCropBounds)
	})
	t.Run("poster_url change clears", func(t *testing.T) {
		next := mk()
		next.Poster.PosterURL = "https://cdn.example/other.jpg"
		sanitizePosterCropGeometry(next, true, curPoster, curCover, false)
		assert.Nil(t, next.Poster.PosterCropBounds)
	})
	t.Run("cover_url change alone preserves (poster is the effective source)", func(t *testing.T) {
		next := mk()
		next.Poster.CoverURL = "https://cdn.example/other-cover.jpg"
		sanitizePosterCropGeometry(next, true, curPoster, curCover, false)
		require.NotNil(t, next.Poster.PosterCropBounds, "fanart churn must not discard a still-valid crop")
	})
	t.Run("cover change clears when cover is the effective source", func(t *testing.T) {
		next := mk()
		next.Poster.PosterURL = ""
		next.Poster.CoverURL = "https://cdn.example/other-cover.jpg"
		sanitizePosterCropGeometry(next, true, "", curCover, false)
		assert.Nil(t, next.Poster.PosterCropBounds)
	})
	t.Run("intent change clears", func(t *testing.T) {
		next := mk()
		next.Poster.ShouldCropPoster = true
		sanitizePosterCropGeometry(next, true, curPoster, curCover, false)
		assert.Nil(t, next.Poster.PosterCropBounds)
	})
	t.Run("invalid geometry dropped", func(t *testing.T) {
		next := mk()
		next.Poster.PosterCropBounds = &models.CropBounds{X: 0.9, Y: 0, Width: 0.5, Height: 1}
		sanitizePosterCropGeometry(next, true, curPoster, curCover, false)
		assert.Nil(t, next.Poster.PosterCropBounds)
		assert.False(t, next.Poster.PosterCropSourceFull)
	})
	t.Run("no stored current keeps geometry", func(t *testing.T) {
		next := mk()
		sanitizePosterCropGeometry(next, false, "", "", false)
		require.NotNil(t, next.Poster.PosterCropBounds)
	})
	t.Run("absent geometry normalizes flag", func(t *testing.T) {
		next := mk()
		next.Poster.PosterCropBounds = nil
		next.Poster.PosterCropSourceFull = true
		sanitizePosterCropGeometry(next, true, curPoster, curCover, false)
		assert.False(t, next.Poster.PosterCropSourceFull)
	})
}

// UpdateMovie (the review-page save) clears geometry when the poster source
// changes and preserves it for unrelated edits when the handler has resolved
// the omitted field onto the payload.
func TestJobEditorUpdateMovie_SourceChangeClearsGeometry(t *testing.T) {
	const filePath = "test.mp4"
	tracker := resultstore.New(1, []string{filePath})
	existing := &models.Movie{ID: "CROPGEO-001", Title: "Subject"}
	existing.Poster.PosterURL = "https://cdn.example/poster.jpg"
	existing.Poster.ShouldCropPoster = false
	existing.Poster.PosterCropBounds = cropGeometryFixture()
	existing.Poster.PosterCropSourceFull = true
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: existing.ID},
		Movie:         existing,
		Status:        models.JobStatusCompleted,
	})
	je := &jobEditorImpl{store: tracker}

	t.Run("source change clears", func(t *testing.T) {
		next := existing.Clone()
		next.Poster.PosterURL = "https://cdn.example/new.jpg"
		require.NoError(t, je.UpdateMovie(context.Background(), filePath, next))
		stored, err := tracker.GetMovieResult(filePath)
		require.NoError(t, err)
		assert.Nil(t, stored.Movie.Poster.PosterCropBounds)
	})

	t.Run("unrelated edit preserves", func(t *testing.T) {
		// Re-seed geometry (previous subtest cleared it).
		existing.Poster.PosterCropBounds = cropGeometryFixture()
		existing.Poster.PosterCropSourceFull = true
		tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: existing.ID},
			Movie:         existing,
			Status:        models.JobStatusCompleted,
		})

		next := existing.Clone()
		next.Title = "Renamed"
		require.NoError(t, je.UpdateMovie(context.Background(), filePath, next))
		stored, err := tracker.GetMovieResult(filePath)
		require.NoError(t, err)
		require.NotNil(t, stored.Movie.Poster.PosterCropBounds, "unchanged source/intent keeps geometry")
		assert.Equal(t, "Renamed", stored.Movie.Title)
	})
}

// Rescrape clears persisted geometry even when the refreshed poster URL is
// unchanged — the new scrape's source image may differ from what the crop
// was measured against.
func TestRescrape_ClearsPosterCropGeometry(t *testing.T) {
	wf := &stubRescrapeWorkflow{
		scrapeResult: &scrape.ScrapeResult{
			Status: scrape.StatusCompleted,
			Movie: &models.Movie{
				ID:    "CROPGEO-001",
				Title: "Rescraped",
				Poster: models.PosterState{
					PosterURL: "https://cdn.example/poster.jpg", // same URL refresh
					CoverURL:  "https://cdn.example/cover.jpg",
				},
			},
		},
	}
	const filePath = "f1.mp4"
	rt := resultstore.New(1, []string{filePath})
	existing := &models.Movie{ID: "CROPGEO-001", Title: "Subject"}
	existing.Poster.PosterURL = "https://cdn.example/poster.jpg"
	existing.Poster.PosterCropBounds = cropGeometryFixture()
	existing.Poster.PosterCropSourceFull = true
	rt.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: existing.ID},
		Movie:         existing,
		Status:        models.JobStatusCompleted,
	})
	inputs := rescrapePhaseInputs{
		WF:        wf,
		ResultMap: rt,
		Finder:    rt,
		JobID:     models.NewJobID(),
	}

	phase := NewRescrapePhase()
	result, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "CROPGEO-001", FilePath: filePath})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, result.Status)

	stored, gErr := rt.GetMovieResult(filePath)
	require.NoError(t, gErr)
	require.NotNil(t, stored.Movie)
	assert.Nil(t, stored.Movie.Poster.PosterCropBounds, "rescrape must clear stored geometry")
	assert.False(t, stored.Movie.Poster.PosterCropSourceFull)
}
