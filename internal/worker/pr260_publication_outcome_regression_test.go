package worker

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

type pr260PublicationSequenceWorkflow struct {
	*stubApplyWorkflow
	mu       sync.Mutex
	attempts int
}

func (w *pr260PublicationSequenceWorkflow) Apply(_ context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.attempts++
	if w.attempts == 1 {
		return &workflow.ApplyResult{Movie: cmd.Movie, FailedStep: "artifact_publication", PrePublication: true},
			errors.Join(errors.New("artifact publication failed"), database.ErrApplyPublicationStale)
	}
	return &workflow.ApplyResult{Movie: cmd.Movie}, nil
}

func TestPR260PublicationStaleWorkerOutcomeAndFreshRetry(t *testing.T) {
	db := newActressEditTestDB(t)
	path := "/incoming/PR260-STALE.mp4"
	movie := &models.Movie{ContentID: "pr260-stale-worker", ID: "PR260-STALE", RenderGeneration: 4}
	require.NoError(t, db.Create(movie).Error)
	wf := &pr260PublicationSequenceWorkflow{stubApplyWorkflow: &stubApplyWorkflow{}}
	repos := db.Repositories()
	deps := NewBatchJobDeps(wf, nil, nil, BatchJobConfig{MaxWorkers: 1, NFOEnabled: true})
	deps.MovieRepo = repos.MovieRepo
	deps.PublicationFence = repos.MovieRepo.(database.ApplyPublicationFencer)
	deps.HistoryRepo = repos.HistoryRepo
	store := NewInMemoryJobStore(WithHistoryRepo(repos.HistoryRepo))
	job := store.CreateJobBatch([]string{path}, &JobConfig{BatchJobDeps: deps})
	job.ResultsWriter().UpdateFileResult(path, &resultstore.MovieResult{
		ResultID:       "stale-result",
		FileMatchInfo:  models.FileMatchInfo{Path: path, MovieID: movie.ID},
		Movie:          movie.Clone(),
		PersistedMovie: true,
		Status:         models.JobStatusCompleted,
	})
	job.Lifecycle().MarkCompleted()
	sub := job.Subscribe()
	defer sub.Close()

	organized := 0
	var first *ApplyFileResult
	cfg := ApplyPhaseConfig{
		Destination:     "/library",
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		OnFileOrganized: func(string) { organized++ },
		PostApplyFunc: func(_ context.Context, _ *ApplyFileContext, got *ApplyFileResult) {
			if first == nil {
				first = got
			}
		},
	}
	require.NoError(t, job.Controller().StartApply(context.Background(), cfg))
	require.NoError(t, job.Controller().Wait())

	require.NotNil(t, first)
	require.ErrorIs(t, first.Err, database.ErrApplyPublicationStale)
	require.NotNil(t, first.Result)
	require.Equal(t, "artifact_publication", first.Result.FailedStep)
	require.True(t, first.Result.PrePublication)
	require.Zero(t, organized)
	require.Equal(t, models.JobStatusCompleted, job.GetStatus().Status)
	failed, err := job.Results().GetMovieResult(path)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, failed.Status)
	require.True(t, failed.PersistedMovie)
drainEvents:
	for {
		select {
		case event := <-sub.Events():
			require.NotEqual(t, StepComplete, event.Step)
		default:
			break drainEvents
		}
	}
	var successHistory int64
	require.NoError(t, db.Model(&models.History{}).
		Where("movie_id = ? AND status = ?", movie.ID, models.HistoryStatusSuccess).
		Count(&successHistory).Error)
	require.Zero(t, successHistory)

	cfg.RetryFilePaths = []string{path}
	require.NoError(t, job.Controller().StartApply(context.Background(), cfg))
	require.NoError(t, job.Controller().Wait())
	require.Equal(t, 1, organized)
	require.Equal(t, models.JobStatusOrganized, job.GetStatus().Status)
	refreshed, err := job.Results().GetMovieResult(path)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusCompleted, refreshed.Status)
}

type pr260EchoApplyWorkflow struct {
	*stubApplyWorkflow
	mu   sync.Mutex
	seen map[string]*models.Movie
	prov map[string]bool
}

func (w *pr260EchoApplyWorkflow) Apply(_ context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	w.mu.Lock()
	w.seen[cmd.Movie.ContentID] = cmd.Movie.Clone()
	w.prov[cmd.Movie.ContentID] = cmd.PersistedMovie
	w.mu.Unlock()
	return &workflow.ApplyResult{Movie: cmd.Movie.Clone()}, nil
}

func TestPR260ApplyRefreshUsesAuthoritativeContentKeys(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := db.Repositories().MovieRepo
	seed := func(contentID, secondaryID, actressName, creditedName string, generation int64) models.Movie {
		t.Helper()
		actress := models.Actress{FirstName: actressName, Verified: true, Origin: "user"}
		require.NoError(t, db.Create(&actress).Error)
		movie := models.Movie{ContentID: contentID, ID: secondaryID, Title: contentID, RenderGeneration: generation}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{actress}))
		require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: contentID, ActressID: actress.ID, CreditedName: creditedName, Origin: "user"}).Error)
		return movie
	}
	seed("content-a", "DUP-1", "Alpha", "Alpha Credit", 11)
	seed("content-b", "DUP-1", "Beta", "Beta Credit", 22)
	deleted := seed("content-deleted", "GONE-1", "Gone", "Gone Credit", 33)
	require.NoError(t, db.Where("content_id = ?", deleted.ContentID).Delete(&models.Movie{}).Error)
	seed("content-empty-id", "", "Empty", "Empty Credit", 44)

	wf := &pr260EchoApplyWorkflow{stubApplyWorkflow: &stubApplyWorkflow{}, seen: map[string]*models.Movie{}, prov: map[string]bool{}}
	inputs := makeApplyInputs(wf)
	inputs.MovieRepo = repo
	updater := newStubUpdater()
	inputs.Updater = updater
	contentIDs := []string{"content-a", "content-b", "content-empty-id", "content-deleted"}
	for _, contentID := range contentIDs {
		path := "/incoming/" + contentID + ".mp4"
		id := "DUP-1"
		if contentID == "content-empty-id" {
			id = ""
		} else if contentID == "content-deleted" {
			id = "GONE-1"
		}
		row := &resultstore.MovieResult{
			ResultID:       "result-" + contentID,
			FileMatchInfo:  models.FileMatchInfo{Path: path, MovieID: id},
			Movie:          &models.Movie{ContentID: contentID, ID: id, Actresses: []models.Actress{{FirstName: "stale"}}, Credits: []models.MovieCredit{{CreditedName: "stale"}}},
			PersistedMovie: contentID == "content-deleted",
			Status:         models.JobStatusCompleted,
		}
		inputs.Results[path] = row
		updater.results[path] = row
	}
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{OrganizeOptions: workflow.OrganizeOptions{Skip: true}})

	for _, tc := range []struct {
		contentID string
		actress   string
		credit    string
		gen       int64
	}{
		{"content-a", "Alpha", "Alpha Credit", 11},
		{"content-b", "Beta", "Beta Credit", 22},
		{"content-empty-id", "Empty", "Empty Credit", 44},
	} {
		got := wf.seen[tc.contentID]
		require.NotNil(t, got)
		require.Equal(t, tc.gen, got.RenderGeneration)
		require.Len(t, got.Actresses, 1)
		require.Equal(t, tc.actress, got.Actresses[0].FirstName)
		require.Len(t, got.Credits, 1)
		require.Equal(t, tc.credit, got.Credits[0].CreditedName)
		require.True(t, wf.prov[tc.contentID])
	}
	require.True(t, wf.prov["content-deleted"], "prior persisted provenance must fail closed after deletion")
	keys := make([]string, 0, len(wf.seen))
	for key := range wf.seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sort.Strings(contentIDs)
	require.Equal(t, contentIDs, keys)
}
