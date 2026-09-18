package worker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type blockingCollisionLookup struct {
	database.CreditCollisionRepositoryInterface
	entered       chan struct{}
	release       chan struct{}
	returnSuccess bool
}

func (r *blockingCollisionLookup) CountOpenByMovieBatch(ctx context.Context, movieIDs []string) (map[string]int64, error) {
	close(r.entered)
	<-r.release
	if r.returnSuccess {
		return map[string]int64{}, nil
	}
	return r.CreditCollisionRepositoryInterface.CountOpenByMovieBatch(ctx, movieIDs)
}

func TestApplyUsesOneAuthoritativeRenderSnapshot(t *testing.T) {
	ctx := context.Background()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "authority.db"), LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	repos := db.Repositories()

	oldCast := models.Actress{FirstName: "Old", LastName: "Cast", Verified: true, Origin: "scrape"}
	currentCast := models.Actress{FirstName: "Current", LastName: "Cast", Verified: true, Origin: "user"}
	require.NoError(t, repos.ActressRepo.Create(ctx, &oldCast))
	require.NoError(t, repos.ActressRepo.Create(ctx, &currentCast))
	old := &models.Movie{
		ContentID: "authority-001", ID: "AUTH-001", Title: "Old title", OriginalFileName: "old-input.mp4",
		Poster:       models.PosterState{PosterURL: "https://example.invalid/old.jpg"},
		Actresses:    []models.Actress{oldCast},
		Credits:      []models.MovieCredit{{ActressID: oldCast.ID, CreditedName: "Old Cast", Actress: &oldCast}},
		Translations: []models.MovieTranslation{{Language: "en", Title: "Old translation"}},
	}
	savedOld, err := repos.MovieRepo.UpsertWithTranslations(ctx, old, nil, nil)
	require.NoError(t, err)
	staleJobMovie := savedOld.Clone()

	current := savedOld.Clone()
	current.Title = "Current title"
	current.OriginalFileName = "current-input.mp4"
	current.Poster.PosterURL = "https://example.invalid/current.jpg"
	current.Actresses = []models.Actress{currentCast}
	current.Credits = []models.MovieCredit{{ActressID: currentCast.ID, CreditedName: "Current Cast", Actress: &currentCast}}
	current.Translations = []models.MovieTranslation{{Language: "en", Title: "Current translation"}}
	_, err = repos.MovieRepo.UpsertWithTranslations(ctx, current, nil, nil)
	require.NoError(t, err)
	savedCurrent, err := repos.MovieRepo.FindByContentID(ctx, current.ContentID)
	require.NoError(t, err)
	require.Greater(t, savedCurrent.RenderGeneration, staleJobMovie.RenderGeneration)

	const inputPath = "/incoming/authority-001.mp4"
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: savedCurrent.Clone()}}
	inputs := makeApplyInputs(wf)
	inputs.MovieRepo = repos.MovieRepo
	row := &resultstore.MovieResult{
		Status:        models.JobStatusCompleted,
		Movie:         staleJobMovie,
		FileMatchInfo: models.FileMatchInfo{Path: inputPath, Name: "authority-001.mp4", MovieID: "AUTH-001"},
	}
	inputs.Results[inputPath] = row
	inputs.Updater.UpdateFileResult(inputPath, row)

	NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{Destination: t.TempDir()})

	cmd := wf.getLastCmd()
	require.NotNil(t, cmd.Movie)
	assert.Equal(t, savedCurrent.Title, cmd.Movie.Title)
	assert.Equal(t, savedCurrent.OriginalFileName, cmd.Movie.OriginalFileName)
	assert.Equal(t, savedCurrent.Poster.PosterURL, cmd.Movie.Poster.PosterURL)
	assert.Equal(t, savedCurrent.RenderGeneration, cmd.Movie.RenderGeneration)
	assert.Equal(t, savedCurrent.Credits, cmd.Movie.Credits)
	require.Len(t, cmd.Movie.Translations, 1)
	assert.Equal(t, "Current translation", cmd.Movie.Translations[0].Title)
	assert.Equal(t, inputPath, cmd.Match.Path)
	final := inputs.Updater.(*stubUpdater).getResult(inputPath)
	require.NotNil(t, final)
	require.NotNil(t, final.Movie)
	assert.Equal(t, savedCurrent.Title, final.Movie.Title)
	assert.Equal(t, savedCurrent.Poster.PosterURL, final.Movie.Poster.PosterURL)
	assert.Equal(t, savedCurrent.Translations, final.Movie.Translations)
}

func TestApplyCancellationAfterSuccessfulCollisionLookupRemainsCancelled(t *testing.T) {
	lookup := &blockingCollisionLookup{entered: make(chan struct{}), release: make(chan struct{}), returnSuccess: true}
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "unused"}}}
	inputs := makeApplyInputs(wf)
	inputs.CollisionRepo = lookup
	inputs.Results["success-race.mp4"] = &resultstore.MovieResult{Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "RACE-001", ContentID: "race-001"}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{Destination: t.TempDir()})
		close(done)
	}()
	<-lookup.entered
	cancel()
	close(lookup.release)
	<-done

	lifecycle := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lifecycle.cancelled)
	assert.False(t, lifecycle.failed)
	assert.Zero(t, wf.getApplyCalled())
}

func TestApplyCancellationDuringCollisionLookupRemainsCancelled(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "cancellation.db"), LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	movie := models.Movie{ContentID: "cancel-001", ID: "CANCEL-001"}
	require.NoError(t, db.Create(&movie).Error)
	actress := models.Actress{FirstName: "Collision", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&actress).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Collision"}
	require.NoError(t, db.Create(&credit).Error)
	require.NoError(t, db.Create(&models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, Status: models.CollisionStatusOpen}).Error)
	lookup := &blockingCollisionLookup{CreditCollisionRepositoryInterface: db.Repositories().CreditCollisionRepo, entered: make(chan struct{}), release: make(chan struct{})}
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "unused"}}}
	inputs := makeApplyInputs(wf)
	inputs.CollisionRepo = lookup
	const path = "/incoming/cancelled.mp4"
	row := &resultstore.MovieResult{Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "CANCEL-001", ContentID: "cancel-001"}}
	inputs.Results[path] = row
	inputs.Updater.UpdateFileResult(path, row)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	failedCallbacks := 0
	phaseCallbacks := 0
	go func() {
		NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{
			Destination:     t.TempDir(),
			OnFileFailed:    func(string, string) { failedCallbacks++ },
			OnPhaseComplete: func(int, int) { phaseCallbacks++ },
		})
		close(done)
	}()
	<-lookup.entered
	cancel()
	close(lookup.release)
	<-done

	lifecycle := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lifecycle.cancelled)
	assert.False(t, lifecycle.failed)
	assert.Zero(t, wf.getApplyCalled())
	assert.Zero(t, failedCallbacks)
	assert.Zero(t, phaseCallbacks)
	final := inputs.Updater.(*stubUpdater).getResult(path)
	require.NotNil(t, final)
	assert.Equal(t, models.JobStatusCompleted, final.Status)
	assert.Empty(t, final.Error)
	broadcaster := inputs.Broadcaster.(*stubBroadcaster)
	for _, event := range broadcaster.events {
		assert.NotEqual(t, StepFailed, event.Step)
	}
}
