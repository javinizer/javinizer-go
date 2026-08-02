package worker

import (
	"context"
	"errors"

	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// midOrganizeCrop simulates a manual crop landing on the live result while a
// pipeline (apply or the scrape persist queue) is still holding its older
// snapshot: the poster source is replaced, the preview URL re-stamped, the
// crop decision cleared, and bounds recorded.
func midOrganizeCrop(t *testing.T, tracker resultstore.ResultUpdater, filePath string) *models.CropBounds {
	t.Helper()
	bounds := &models.CropBounds{X: 11, Y: 22, Width: 300, Height: 450, ImageWidth: 1000, ImageHeight: 1500, MaxPosterHeight: 800}
	require.NoError(t, tracker.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		require.NotNil(t, current.Movie)
		m := current.Movie.Clone()
		m.Poster.PosterURL = "https://live.example/user-poster.jpg"
		m.Poster.CoverURL = "https://live.example/user-cover.jpg"
		m.Poster.CroppedPosterURL = "/api/v1/temp/posters/job/ABC-001.jpg?v=9"
		m.Poster.ShouldCropPoster = false
		m.Poster.CropBounds = bounds
		current.Movie = m
		return current, nil
	}))
	return bounds
}

func assertLivePosterPreserved(t *testing.T, got *models.Movie, bounds *models.CropBounds) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, "https://live.example/user-poster.jpg", got.Poster.PosterURL, "the interleaved source edit must win over the stale snapshot")
	assert.Equal(t, "https://live.example/user-cover.jpg", got.Poster.CoverURL)
	assert.False(t, got.Poster.ShouldCropPoster)
	assert.Equal(t, "/api/v1/temp/posters/job/ABC-001.jpg?v=9", got.Poster.CroppedPosterURL)
	require.NotNil(t, got.Poster.CropBounds, "the mid-pipeline manual crop must survive the write-back")
	assert.Equal(t, *bounds, *got.Poster.CropBounds)
}

// TestInterpretApplyResult_SuccessPreservesMidOrganizeCrop pins F-C's success
// path: the AtomicUpdateFileResult write-back used to blindly store the
// apply-start snapshot (result.Movie.Clone()), erasing any crop taken while
// the file was being organized.
func TestInterpretApplyResult_SuccessPreservesMidOrganizeCrop(t *testing.T) {
	const filePath = "/input/ABC-001.mp4"
	tracker := resultstore.New(1, []string{filePath})
	snapshot := &models.Movie{ID: "ABC-001", Title: "Scraped", Poster: models.PosterState{
		PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
	}}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         snapshot,
	})

	inputs := applyPhaseInputs{
		JobID:       models.NewJobID(),
		Broadcaster: &stubBroadcaster{},
		Updater:     tracker,
	}
	cfg := ApplyPhaseConfig{}
	afc := &ApplyFileContext{FilePath: filePath, Match: models.FileMatchInfo{Path: filePath, MovieID: "ABC-001"}}

	// The crop lands while the workflow.Apply call is still holding the
	// apply-start snapshot.
	bounds := midOrganizeCrop(t, tracker, filePath)

	pipeline := &models.Movie{ID: "ABC-001", Title: "Organized", Description: "pipeline-updated", Poster: models.PosterState{
		PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
	}}
	outcome := interpretApplyResult(filePath, snapshot, time.Now(), time.Minute, inputs, cfg,
		context.Background(), afc, &workflow.ApplyResult{Movie: pipeline}, nil)

	require.True(t, outcome.Success, "outcome: %+v", outcome)
	stored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assertLivePosterPreserved(t, stored.Movie, bounds)
	assert.Equal(t, "Organized", stored.Movie.Title, "pipeline-computed fields must not regress to live state")
	assert.Equal(t, "pipeline-updated", stored.Movie.Description)
}

// TestInterpretApplyResult_FailurePreservesMidOrganizeCrop pins F-C's failure
// path: apply errors preserved the scrape-phase movie via a wholesale
// UpdateFileResult — the same stale-snapshot clobber as the success path.
func TestInterpretApplyResult_FailurePreservesMidOrganizeCrop(t *testing.T) {
	const filePath = "/input/ABC-002.mp4"
	tracker := resultstore.New(1, []string{filePath})
	snapshot := &models.Movie{ID: "ABC-002", Title: "Scraped", Poster: models.PosterState{
		PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
	}}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID:      "res-abc-002",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "ABC-002"},
		Status:        models.JobStatusCompleted,
		Movie:         snapshot,
	})

	inputs := applyPhaseInputs{
		JobID:       models.NewJobID(),
		Broadcaster: &stubBroadcaster{},
		Updater:     tracker,
	}
	cfg := ApplyPhaseConfig{}
	afc := &ApplyFileContext{FilePath: filePath, Match: models.FileMatchInfo{Path: filePath, MovieID: "ABC-002"}}

	bounds := midOrganizeCrop(t, tracker, filePath)

	outcome := interpretApplyResult(filePath, snapshot, time.Now(), time.Minute, inputs, cfg,
		context.Background(), afc, nil, errors.New("simulated apply failure"))

	require.True(t, outcome.Failed, "outcome: %+v", outcome)
	stored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusFailed, stored.Status)
	assert.Equal(t, "res-abc-002", stored.ResultID, "the failure write-back must preserve ResultID (review lookups key on it)")
	require.NotNil(t, stored.Movie, "the scrape-phase movie is preserved on failed applies")
	assertLivePosterPreserved(t, stored.Movie, bounds)
	assert.Equal(t, "Scraped", stored.Movie.Title, "the failure path keeps the snapshot's metadata, poster fields aside")
}

// stripBoundsPersistRepo simulates the DB round trip for the scrape persist
// pool: the saved movie comes back with normalized fields but WITHOUT the
// runtime-only CropBounds (gorm:"-").
type stripBoundsPersistRepo struct{ savedTitle string }

func (r stripBoundsPersistRepo) Create(_ context.Context, _ *models.Movie) error { return nil }
func (r stripBoundsPersistRepo) Update(_ context.Context, _ *models.Movie) error { return nil }
func (r stripBoundsPersistRepo) Upsert(_ context.Context, m *models.Movie) (*models.Movie, error) {
	return m, nil
}
func (r stripBoundsPersistRepo) UpsertWithTranslations(_ context.Context, m *models.Movie, _ []models.GenreTranslationData, _ []models.ActressTranslationData) (*models.Movie, error) {
	saved := m.Clone()
	saved.Title = r.savedTitle
	saved.Poster.CropBounds = nil // gorm:"-" — never round-trips through the movies table
	return saved, nil
}
func (r stripBoundsPersistRepo) FindByID(_ context.Context, _ string) (*models.Movie, error) {
	return nil, nil
}
func (r stripBoundsPersistRepo) FindByContentID(_ context.Context, _ string) (*models.Movie, error) {
	return nil, nil
}
func (r stripBoundsPersistRepo) Delete(_ context.Context, _ string) error { return nil }
func (r stripBoundsPersistRepo) List(_ context.Context, _, _ int) ([]models.Movie, error) {
	return nil, nil
}

// TestPersistScrapeOutcome_PreservesInterleavedCrop pins F-D: the persist
// pool's AtomicUpdateFileResult used to wholesale-replace current.Movie with
// the saved DB clone — erasing a crop (or source edit) that landed between
// the scrape commit and the persist write-back.
func TestPersistScrapeOutcome_PreservesInterleavedCrop(t *testing.T) {
	const filePath = "/input/ABC-003.mp4"
	tracker := resultstore.New(1, []string{filePath})
	scraped := &models.Movie{ID: "ABC-003", Title: "Scraped", Poster: models.PosterState{
		PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
	}}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "ABC-003"},
		Status:        models.JobStatusCompleted,
		Movie:         scraped,
	})

	// The interleaved edit lands after the scrape committed its movie but
	// before the persist pool's write-back runs.
	bounds := midOrganizeCrop(t, tracker, filePath)

	inputs := scrapePhaseInputs{
		JobID:       models.NewJobID(),
		MovieRepo:   stripBoundsPersistRepo{savedTitle: "Scraped (normalized)"},
		Broadcaster: &stubBroadcaster{},
		Updater:     tracker,
	}
	outcome := scrapeFileOutcome{
		FilePath: filePath,
		MovieID:  "ABC-003",
		Success:  true,
		Result:   &scrape.ScrapeResult{Movie: scraped, Status: scrape.StatusCompleted},
	}

	persistScrapeOutcome(context.Background(), outcome, inputs, nil)

	stored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.True(t, stored.Persisted)
	assertLivePosterPreserved(t, stored.Movie, bounds)
	assert.Equal(t, "Scraped (normalized)", stored.Movie.Title,
		"the DB-normalized metadata fields must still reach the result")
}

// TestMergeLivePosterState_NilGuards pins the helper's degeneration: a nil
// live movie (result lost mid-pipeline) or nil destination leaves the
// write-back untouched.
func TestMergeLivePosterState_NilGuards(t *testing.T) {
	dst := &models.Movie{Poster: models.PosterState{PosterURL: "a", CropBounds: &models.CropBounds{X: 1}}}
	mergeLivePosterState(dst, nil)
	assert.Equal(t, "a", dst.Poster.PosterURL)
	require.NotNil(t, dst.Poster.CropBounds)
	mergeLivePosterState(nil, &models.Movie{}) // must not panic

	// A live movie without bounds clears a stale snapshot's bounds (an edit
	// that invalidated them landed mid-pipeline).
	dst2 := &models.Movie{Poster: models.PosterState{PosterURL: "a", CropBounds: &models.CropBounds{X: 1}}}
	mergeLivePosterState(dst2, &models.Movie{Poster: models.PosterState{PosterURL: "b"}})
	assert.Equal(t, "b", dst2.Poster.PosterURL)
	assert.Nil(t, dst2.Poster.CropBounds, "live state without bounds must clear the snapshot's stale bounds")
}

// TestMergeLivePosterState_SkipsIdentityMismatch pins F5's guard: when the
// live result was re-keyed mid-pipeline (a rescrape/PATCH committed a
// corrected match, moving Movie.ID from A to B) while the pipeline write-back
// was cloned from A's snapshot, the merge must NOT blend B's poster identity
// into A's movie — that franken-movie would attach B's source/crop to A's
// metadata. On mismatch the write-back keeps its own snapshot poster state.
func TestMergeLivePosterState_SkipsIdentityMismatch(t *testing.T) {
	dst := &models.Movie{ID: "AAA-111", Title: "Snapshot A", Poster: models.PosterState{
		PosterURL:        "https://a.example/poster.jpg",
		CoverURL:         "https://a.example/cover.jpg",
		ShouldCropPoster: true,
		CroppedPosterURL: "/a-preview.jpg?v=1",
		CropBounds:       &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4},
	}}
	live := &models.Movie{ID: "BBB-222", Title: "Live B", Poster: models.PosterState{
		PosterURL:        "https://b.example/poster.jpg",
		CoverURL:         "https://b.example/cover.jpg",
		ShouldCropPoster: false,
		CroppedPosterURL: "/b-preview.jpg?v=9",
		CropBounds:       &models.CropBounds{X: 9, Y: 9, Width: 9, Height: 9},
	}}

	mergeLivePosterState(dst, live)

	assert.Equal(t, "https://a.example/poster.jpg", dst.Poster.PosterURL,
		"a rekeyed live result must not override the write-back's poster source")
	assert.Equal(t, "https://a.example/cover.jpg", dst.Poster.CoverURL)
	assert.True(t, dst.Poster.ShouldCropPoster)
	assert.Equal(t, "/a-preview.jpg?v=1", dst.Poster.CroppedPosterURL)
	require.NotNil(t, dst.Poster.CropBounds)
	assert.Equal(t, models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4}, *dst.Poster.CropBounds)
	assert.Equal(t, "Snapshot A", dst.Title)

	// Same-identity merges still happen (the normal mid-pipeline edit case).
	live.ID = "AAA-111"
	mergeLivePosterState(dst, live)
	assert.Equal(t, "https://b.example/poster.jpg", dst.Poster.PosterURL)
}
