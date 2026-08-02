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

// rekeyLiveResult moves the live result's movie to a NEW identity (B) while
// the pipeline still holds its A snapshot — what a rescrape or whole-movie
// edit committing a corrected match does mid-flight.
func rekeyLiveResult(t *testing.T, tracker resultstore.ResultUpdater, filePath, newID string) *models.Movie {
	t.Helper()
	var liveB *models.Movie
	require.NoError(t, tracker.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		require.NotNil(t, current.Movie)
		liveB = current.Movie.Clone()
		liveB.ID = newID
		current.Movie = liveB
		current.FileMatchInfo.MovieID = newID
		return current, nil
	}))
	return liveB
}

// TestInterpretApplyResult_SuccessKeepsLiveMovieOnRekey pins P2-5 on the
// apply SUCCESS path: when the live result was re-keyed mid-apply, the
// pipeline write-back must NOT overwrite the live movie with the apply-start
// snapshot at all (previously it stamped A's clone over B wholesale).
func TestInterpretApplyResult_SuccessKeepsLiveMovieOnRekey(t *testing.T) {
	const filePath = "/input/RK-001.mp4"
	tracker := resultstore.New(1, []string{filePath})
	snapshotA := &models.Movie{ID: "AAA-111", Title: "Scraped A", Poster: models.PosterState{
		PosterURL: "https://a.example/poster.jpg", ShouldCropPoster: true,
	}}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "AAA-111"},
		Status:        models.JobStatusCompleted,
		Movie:         snapshotA,
	})

	inputs := applyPhaseInputs{
		JobID:       models.NewJobID(),
		Broadcaster: &stubBroadcaster{},
		Updater:     tracker,
	}
	afc := &ApplyFileContext{FilePath: filePath, Match: models.FileMatchInfo{Path: filePath, MovieID: "AAA-111"}}

	liveB := rekeyLiveResult(t, tracker, filePath, "BBB-222")

	pipelineA := &models.Movie{ID: "AAA-111", Title: "Organized A", Poster: models.PosterState{
		PosterURL: "https://a.example/poster.jpg", ShouldCropPoster: true,
	}}
	outcome := interpretApplyResult(filePath, snapshotA, time.Now(), time.Minute, inputs, ApplyPhaseConfig{},
		context.Background(), afc, &workflow.ApplyResult{Movie: pipelineA}, nil)
	require.True(t, outcome.Success, "outcome: %+v", outcome)

	stored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "BBB-222", stored.Movie.ID,
		"the write-back must keep the live re-keyed movie — A's snapshot must not stamp over B")
	assert.Equal(t, liveB.Title, stored.Movie.Title)
	assert.Equal(t, liveB.Poster.PosterURL, stored.Movie.Poster.PosterURL)
}

// TestInterpretApplyResult_FailureKeepsLiveMovieOnRekey pins P2-5 on the
// apply FAILURE path: on an identity mismatch only the pipeline-owned
// non-movie fields (status/error/timestamps) move — the live re-keyed movie
// survives.
func TestInterpretApplyResult_FailureKeepsLiveMovieOnRekey(t *testing.T) {
	const filePath = "/input/RK-002.mp4"
	tracker := resultstore.New(1, []string{filePath})
	snapshotA := &models.Movie{ID: "AAA-111", Title: "Scraped A", Poster: models.PosterState{
		PosterURL: "https://a.example/poster.jpg",
	}}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID:      "res-rk-002",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "AAA-111"},
		Status:        models.JobStatusCompleted,
		Movie:         snapshotA,
	})

	inputs := applyPhaseInputs{
		JobID:       models.NewJobID(),
		Broadcaster: &stubBroadcaster{},
		Updater:     tracker,
	}
	afc := &ApplyFileContext{FilePath: filePath, Match: models.FileMatchInfo{Path: filePath, MovieID: "AAA-111"}}

	rekeyLiveResult(t, tracker, filePath, "BBB-222")

	outcome := interpretApplyResult(filePath, snapshotA, time.Now(), time.Minute, inputs, ApplyPhaseConfig{},
		context.Background(), afc, nil, errors.New("simulated apply failure"))
	require.True(t, outcome.Failed, "outcome: %+v", outcome)

	stored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	require.NotNil(t, stored.Movie, "the live movie must survive the mismatching failure write-back")
	assert.Equal(t, "BBB-222", stored.Movie.ID)
	assert.Equal(t, models.JobStatusFailed, stored.Status, "pipeline-owned status still moves")
	assert.NotEmpty(t, stored.Error, "pipeline-owned error still moves")
	assert.False(t, stored.StartedAt.IsZero(), "pipeline-owned Start timestamp still moves")
	require.NotNil(t, stored.EndedAt, "pipeline-owned End timestamp still moves")
}

// TestPersistScrapeOutcome_KeepsLiveMovieOnRekey pins P2-5 on the scrape
// persist-pool write-back: a mid-persist rekey keeps the live movie while
// the pipeline-owned Persisted flag still flips.
func TestPersistScrapeOutcome_KeepsLiveMovieOnRekey(t *testing.T) {
	const filePath = "/input/RK-003.mp4"
	tracker := resultstore.New(1, []string{filePath})
	scrapedA := &models.Movie{ID: "AAA-111", Title: "Scraped A", Poster: models.PosterState{
		PosterURL: "https://a.example/poster.jpg", ShouldCropPoster: true,
	}}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "AAA-111"},
		Status:        models.JobStatusCompleted,
		Movie:         scrapedA,
	})

	liveB := rekeyLiveResult(t, tracker, filePath, "BBB-222")

	inputs := scrapePhaseInputs{
		JobID:       models.NewJobID(),
		MovieRepo:   stripBoundsPersistRepo{savedTitle: "Scraped A (normalized)"},
		Broadcaster: &stubBroadcaster{},
		Updater:     tracker,
	}
	outcome := scrapeFileOutcome{
		FilePath: filePath,
		MovieID:  "AAA-111",
		Success:  true,
		Result:   &scrape.ScrapeResult{Movie: scrapedA, Status: scrape.StatusCompleted},
	}

	persistScrapeOutcome(context.Background(), outcome, inputs, nil)

	stored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.True(t, stored.Persisted, "the pipeline-owned Persisted flag still moves")
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "BBB-222", stored.Movie.ID,
		"the DB round-trip must not stamp A's normalized clone over the re-keyed live movie")
	assert.Equal(t, liveB.Title, stored.Movie.Title)
}

// TestWithFileRecovery_PanicKeepsLiveMovieOnRekey pins the panic write-back's
// poster-state discipline (P2-5's failure sibling): a panicking apply worker
// whose snapshot was re-keyed mid-flight must NOT overwrite the live movie —
// only the pipeline-owned status/error/timestamps move.
func TestWithFileRecovery_PanicKeepsLiveMovieOnRekey(t *testing.T) {
	const filePath = "/input/RK-004.mp4"
	tracker := resultstore.New(1, []string{filePath})
	snapshotA := &models.Movie{ID: "AAA-111", Title: "Scraped A"}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "AAA-111"},
		Status:        models.JobStatusCompleted,
		Movie:         snapshotA,
	})
	rekeyLiveResult(t, tracker, filePath, "BBB-222")

	outcome := &applyFileOutcome{}
	rc := recoveryContext{
		jobID:     models.NewJobID(),
		filePath:  filePath,
		fmi:       models.FileMatchInfo{Path: filePath, MovieID: "AAA-111"},
		movie:     snapshotA,
		updater:   tracker,
		startTime: time.Now(),
	}
	func() {
		defer withFileRecovery(rc, outcome)()
		panic("simulated apply panic")
	}()

	assert.True(t, outcome.Panic, "outcome: %+v", outcome)
	stored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "BBB-222", stored.Movie.ID, "the panic write-back must not stamp A's snapshot over B")
	assert.Equal(t, models.JobStatusFailed, stored.Status)
	assert.Contains(t, stored.Error, "simulated apply panic")
}

// TestWithFileRecovery_PanicPreservesMovieOnMatch keeps the legacy behavior
// when identities match: the panic write-back retains the prior scrape-phase
// movie (merged with live poster state) so failed-apply rows keep their
// movie payload.
func TestWithFileRecovery_PanicPreservesMovieOnMatch(t *testing.T) {
	const filePath = "/input/RK-005.mp4"
	tracker := resultstore.New(1, []string{filePath})
	snapshot := &models.Movie{ID: "MATCH-1", Title: "Scraped", Poster: models.PosterState{
		PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
	}}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "MATCH-1"},
		Status:        models.JobStatusCompleted,
		Movie:         snapshot,
	})
	bounds := midOrganizeCrop(t, tracker, filePath)

	outcome := &applyFileOutcome{}
	rc := recoveryContext{
		jobID:    models.NewJobID(),
		filePath: filePath,
		fmi:      models.FileMatchInfo{Path: filePath, MovieID: "MATCH-1"},
		movie:    snapshot,
		updater:  tracker,
	}
	func() {
		defer withFileRecovery(rc, outcome)()
		panic("boom")
	}()

	stored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "MATCH-1", stored.Movie.ID)
	assertLivePosterPreserved(t, stored.Movie, bounds)
	assert.Equal(t, models.JobStatusFailed, stored.Status)
}
