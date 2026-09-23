package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type timeoutSuccessWorkflowPR260 struct {
	result *scrape.ScrapeResult
}

func (w *timeoutSuccessWorkflowPR260) Scrape(ctx context.Context, _ scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	<-ctx.Done()
	return w.result, nil, nil
}
func (*timeoutSuccessWorkflowPR260) Apply(context.Context, workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	return nil, nil
}
func (*timeoutSuccessWorkflowPR260) Preview(context.Context, workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (*timeoutSuccessWorkflowPR260) Compare(context.Context, workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (*timeoutSuccessWorkflowPR260) ScanAndMatch(context.Context, workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func TestWithCollisionRepoPR260(t *testing.T) {
	repo := mocks.NewMockCreditCollisionRepositoryInterface(t)
	store := &JobStore{}
	WithCollisionRepo(repo)(store)
	require.Same(t, repo, store.collisionRepo)
}

func TestRescrapeSingleConvertsLateSuccessToTimeoutPR260(t *testing.T) {
	for _, result := range []*scrape.ScrapeResult{nil, {Movie: &models.Movie{ID: "late"}}} {
		wf := &timeoutSuccessWorkflowPR260{result: result}
		inputs := makeRescrapeInputs(wf)
		inputs.Concurrency.RequestTimeout = time.Millisecond
		got, _, err := NewRescrapePhase().ScrapeSingle(context.Background(), inputs, "movie.mp4", scrape.ScrapeCmd{MovieID: "MOVIE"})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.NotNil(t, got)
		assert.Equal(t, scrape.StatusFailed, got.Status)
		assert.Equal(t, scrapeTimeoutMessage, got.Message)
	}
}

func TestScrapePhaseConvertsNilLateSuccessToTimeoutPR260(t *testing.T) {
	inputs := makeInputs(nil)
	inputs.WF = &timeoutSuccessWorkflowPR260{}
	inputs.Concurrency.RequestTimeout = time.Millisecond
	NewScrapePhase().Run(context.Background(), inputs, []string{"movie.mp4"}, ScrapePhaseConfig{})
	result := inputs.Updater.(*stubUpdater).getResult("movie.mp4")
	require.NotNil(t, result)
	assert.Equal(t, models.JobStatusFailed, result.Status)
}

func TestApplyPhaseCollisionLookupFailureBlocksCandidatesPR260(t *testing.T) {
	repo := mocks.NewMockCreditCollisionRepositoryInterface(t)
	repo.EXPECT().CountOpenByMovieBatch(mock.Anything, mock.Anything).Return(nil, errors.New("database unavailable"))
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "clear"}}}
	inputs := makeApplyInputs(wf)
	inputs.CollisionRepo = repo
	inputs.Results = map[string]*resultstore.MovieResult{
		"nil.mp4":   {Status: models.JobStatusCompleted},
		"empty.mp4": {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "empty"}},
		"one.mp4":   {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "one", ContentID: "shared"}},
		"two.mp4":   {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "two", ContentID: "shared"}},
	}
	var organized, failed int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		Destination: "/output",
		OnPhaseComplete: func(org, fail int) {
			organized, failed = org, fail
		},
	})
	assert.Equal(t, 1, wf.getApplyCalled())
	assert.Equal(t, 1, organized)
	assert.Equal(t, 2, failed)
}

func TestApplyPhaseCollisionLookupFailureCountsRetryBlockedFilePR260(t *testing.T) {
	repo := mocks.NewMockCreditCollisionRepositoryInterface(t)
	repo.EXPECT().CountOpenByMovieBatch(mock.Anything, mock.Anything).Return(nil, errors.New("database unavailable"))
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "clear"}}}
	inputs := makeApplyInputs(wf)
	inputs.CollisionRepo = repo
	inputs.Results = map[string]*resultstore.MovieResult{
		"blocked.mp4": {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "blocked", ContentID: "blocked"}},
		"clear.mp4":   {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "clear"}},
	}
	var organized, failed int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		Destination:    "/output",
		RetryFilePaths: []string{"blocked.mp4", "clear.mp4"},
		OnPhaseComplete: func(org, fail int) {
			organized, failed = org, fail
		},
	})
	assert.Equal(t, 1, wf.getApplyCalled())
	assert.Equal(t, 1, organized)
	assert.Equal(t, 1, failed)
}

func TestApplyPhaseCollisionLookupFailureWithOnlyIDsFailsRunPR260(t *testing.T) {
	repo := mocks.NewMockCreditCollisionRepositoryInterface(t)
	repo.EXPECT().CountOpenByMovieBatch(mock.Anything, mock.Anything).Return(nil, errors.New("database unavailable"))
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "unused"}}}
	inputs := makeApplyInputs(wf)
	inputs.CollisionRepo = repo
	inputs.Results = map[string]*resultstore.MovieResult{
		"one.mp4": {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "one", ContentID: "one"}},
		"two.mp4": {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "two", ContentID: "two"}},
	}
	called := false
	var organized, failed int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		Destination: "/output",
		OnPhaseComplete: func(org, fail int) {
			called = true
			organized, failed = org, fail
		},
	})
	lifecycle := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, called)
	assert.Zero(t, organized)
	assert.Equal(t, 2, failed)
	assert.True(t, lifecycle.failed)
	assert.False(t, lifecycle.completed)
	assert.False(t, lifecycle.organized)
	assert.Zero(t, wf.getApplyCalled())
}
func TestApplyPhaseCollisionCountsBlockOnlyOpenMoviesPR260(t *testing.T) {
	repo := mocks.NewMockCreditCollisionRepositoryInterface(t)
	repo.EXPECT().CountOpenByMovieBatch(mock.Anything, mock.Anything).Return(map[string]int64{"blocked": 1, "clear": 0}, nil)
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "clear", ContentID: "clear"}}}
	inputs := makeApplyInputs(wf)
	inputs.CollisionRepo = repo
	inputs.Results = map[string]*resultstore.MovieResult{
		"blocked.mp4": {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "blocked", ContentID: "blocked"}},
		"clear.mp4":   {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "clear", ContentID: "clear"}},
	}
	var organized, failed int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		Destination: "/output",
		OnPhaseComplete: func(org, fail int) {
			organized, failed = org, fail
		},
	})
	assert.Equal(t, 1, wf.getApplyCalled())
	assert.Equal(t, 1, organized)
	assert.Equal(t, 1, failed)
	lifecycle := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lifecycle.completed)
	assert.False(t, lifecycle.organized)
}

func TestApplyPhaseRefreshesPersistedCreditsPR260(t *testing.T) {
	repo := mocks.NewMockMovieRepositoryInterface(t)
	canonical := models.Actress{ID: 23, DMMID: 9023, FirstName: "Canonical", LastName: "Actress", Verified: true}
	persisted := &models.Movie{
		ID:        "movie-db-id",
		ContentID: "refresh-001",
		Actresses: []models.Actress{canonical},
		Credits:   []models.MovieCredit{{ActressID: canonical.ID, Actress: &canonical, CreditedName: "Canonical Actress"}},
	}
	repo.EXPECT().FindByContentID(mock.Anything, "refresh-001").Return(persisted, nil)
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "movie-db-id"}}}
	inputs := makeApplyInputs(wf)
	inputs.MovieRepo = repo
	inputs.Results["refresh.mp4"] = &resultstore.MovieResult{
		Status: models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:        "movie-db-id",
			ContentID: "refresh-001",
			Actresses: []models.Actress{{ID: 7, FirstName: "Stale"}},
			Credits:   []models.MovieCredit{{ActressID: 7, CreditedName: "Stale"}},
		},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{Destination: "/output"})

	cmd := wf.getLastCmd()
	require.NotNil(t, cmd.Movie)
	require.Len(t, cmd.Movie.Actresses, 1)
	require.Len(t, cmd.Movie.Credits, 1)
	assert.Equal(t, canonical.ID, cmd.Movie.Actresses[0].ID)
	assert.Equal(t, canonical.ID, cmd.Movie.Credits[0].ActressID)
	assert.Equal(t, "Canonical Actress", cmd.Movie.Credits[0].CreditedName)
}

func TestApplyPhaseCollisionGatePublishesPerFileFailurePR260(t *testing.T) {
	repo := mocks.NewMockCreditCollisionRepositoryInterface(t)
	repo.EXPECT().CountOpenByMovieBatch(mock.Anything, mock.Anything).Return(map[string]int64{"blocked": 1, "clear": 0}, nil)
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "clear", ContentID: "clear"}}}
	inputs := makeApplyInputs(wf)
	inputs.CollisionRepo = repo
	blocked := &resultstore.MovieResult{Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "blocked", ContentID: "blocked"}}
	clear := &resultstore.MovieResult{Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "clear", ContentID: "clear"}}
	inputs.Results = map[string]*resultstore.MovieResult{"blocked.mp4": blocked, "clear.mp4": clear}
	inputs.Updater.UpdateFileResult("blocked.mp4", blocked)
	inputs.Updater.UpdateFileResult("clear.mp4", clear)

	var organized, failed int
	var failedPath, failedReason string
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		Destination:     "/output",
		OnPhaseComplete: func(org, fail int) { organized, failed = org, fail },
		OnFileFailed:    func(path, reason string) { failedPath, failedReason = path, reason },
	})

	row := inputs.Updater.(*stubUpdater).getResult("blocked.mp4")
	require.NotNil(t, row)
	assert.Equal(t, models.JobStatusFailed, row.Status)
	assert.Equal(t, string(models.ScraperErrorKindBlocked), row.ErrorCode)
	assert.Contains(t, row.Error, "open credit collision")
	assert.Equal(t, 1, organized)
	assert.Equal(t, 1, failed)
	assert.True(t, inputs.Lifecycle.(*stubLifecycle).completed)
	assert.False(t, inputs.Lifecycle.(*stubLifecycle).organized)
	assert.Equal(t, 1, wf.getApplyCalled())
	assert.Equal(t, "blocked.mp4", failedPath)
	assert.Contains(t, failedReason, "open credit collision")
	broadcaster := inputs.Broadcaster.(*stubBroadcaster)
	assert.Condition(t, func() bool {
		for _, event := range broadcaster.events {
			if event.Step == StepFailed && event.MovieID == "blocked" && strings.Contains(event.Message, "open credit collision") {
				return true
			}
		}
		return false
	})
}

func TestApplyPhaseRealCollisionResolutionRetriesOnlyBlockedFilePR260(t *testing.T) {
	db := newActressEditTestDB(t)
	ctx := context.Background()
	actress := models.Actress{FirstName: "Gate", Verified: true}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "blocked", ID: "blocked"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Gate"}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Reported", CanonicalValue: "Gate", Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)

	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "applied"}}}
	inputs := makeApplyInputs(wf)
	inputs.CollisionRepo = database.NewCreditCollisionRepository(db)
	blocked := &resultstore.MovieResult{Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "blocked", ContentID: "blocked"}}
	clear := &resultstore.MovieResult{Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "clear", ContentID: "clear"}}
	inputs.Results = map[string]*resultstore.MovieResult{"blocked.mp4": blocked, "clear.mp4": clear}
	inputs.Updater.UpdateFileResult("blocked.mp4", blocked)
	inputs.Updater.UpdateFileResult("clear.mp4", clear)

	NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{Destination: "/output"})
	require.Equal(t, 1, wf.getApplyCalled())
	require.Equal(t, models.JobStatusFailed, inputs.Updater.(*stubUpdater).getResult("blocked.mp4").Status)

	require.NoError(t, inputs.CollisionRepo.Resolve(ctx, collision.ID, models.CollisionResolutionKeepIdentity))
	NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{Destination: "/output", RetryFilePaths: []string{"blocked.mp4"}})
	require.Equal(t, 2, wf.getApplyCalled(), "the already-clear file must not run twice")
	require.Equal(t, models.JobStatusCompleted, inputs.Updater.(*stubUpdater).getResult("blocked.mp4").Status)
}
