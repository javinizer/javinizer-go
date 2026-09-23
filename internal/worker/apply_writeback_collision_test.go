package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyWritebackPreservesResolvedCreditIdentityPR260(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()
	const filePath = "/source/PR260-WB.mp4"
	const contentID = "pr260-writeback"
	const movieID = "PR260-WB"

	source := models.Actress{FirstName: "Source", LastName: "Identity", Verified: true, Origin: "scrape"}
	target := models.Actress{FirstName: "Canonical", LastName: "Identity", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&target).Error)
	movie := models.Movie{ContentID: contentID, ID: movieID, Title: "Persisted title"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: contentID, ActressID: source.ID, CreditedName: "Source Identity", Origin: "scrape"}
	require.NoError(t, db.Create(&credit).Error)
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{source}))
	collision := models.CreditCollision{
		CreditID:       credit.ID,
		MovieContentID: contentID,
		Field:          models.CreditFieldCreditedName,
		ReportedValue:  "Source Identity",
		CanonicalValue: "Canonical Identity",
		Status:         models.CollisionStatusOpen,
		Occurrences:    1,
	}
	require.NoError(t, db.Create(&collision).Error)
	_, err := database.NewCollisionService(db).Resolve(ctx, collision.ID, models.CollisionResolutionReassign, target.ID)
	require.NoError(t, err)
	authoritative, err := repos.MovieRepo.FindByID(ctx, movieID)
	require.NoError(t, err)
	require.Len(t, authoritative.Actresses, 1)
	require.Len(t, authoritative.Credits, 1)
	require.Equal(t, target.ID, authoritative.Actresses[0].ID)
	require.Equal(t, target.ID, authoritative.Credits[0].ActressID)

	old := &models.Movie{
		ID:               movieID,
		ContentID:        contentID,
		Title:            "Job title",
		OriginalFileName: "job-file.mkv",
		TrailerURL:       "job-trailer",
		Poster: models.PosterState{
			PosterURL:        "job-poster",
			CroppedPosterURL: "job-crop",
		},
		Screenshots: []string{"job-shot"},
		Actresses:   []models.Actress{source},
		Credits: []models.MovieCredit{{
			MovieContentID: contentID,
			ActressID:      source.ID,
			CreditedName:   "Source Identity",
			Actress:        &source,
		}},
	}
	live := old.Clone()
	live.Title = "Review title"
	live.Poster.CroppedPosterURL = "Review crop"
	live.Screenshots = []string{"review-shot"}
	store := resultstore.New(1, []string{filePath})
	store.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID: "pr260-result",
		Revision: 1,
		Status:   models.JobStatusCompleted,
		Movie:    live,
		FileMatchInfo: models.FileMatchInfo{
			Path:    filePath,
			MovieID: movieID,
		},
	})
	phaseOut := old.Clone()
	phaseOut.Actresses = authoritative.Actresses
	phaseOut.Credits = authoritative.Credits
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: phaseOut}}
	inputs := makeApplyInputs(wf)
	inputs.Updater = store
	inputs.MovieRepo = repos.MovieRepo
	inputs.Results = map[string]*resultstore.MovieResult{
		filePath: {
			ResultID: "pr260-result",
			Revision: 1,
			Status:   models.JobStatusCompleted,
			Movie:    old.Clone(),
			FileMatchInfo: models.FileMatchInfo{
				Path:    filePath,
				MovieID: movieID,
			},
		},
	}
	NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{Destination: "/output"})

	final, err := store.GetMovieResult(filePath)
	require.NoError(t, err)
	require.NotNil(t, final.Movie)
	require.Len(t, final.Movie.Actresses, 1)
	require.Len(t, final.Movie.Credits, 1)
	assert.Equal(t, uint64(2), final.Revision)
	assert.Equal(t, target.ID, final.Movie.Actresses[0].ID)
	assert.Equal(t, target.ID, final.Movie.Credits[0].ActressID)
	assert.Equal(t, "Canonical", final.Movie.Actresses[0].FirstName)
	assert.Equal(t, "Review title", final.Movie.Title)
	assert.Equal(t, "Review crop", final.Movie.Poster.CroppedPosterURL)
	assert.Equal(t, "job-file.mkv", final.Movie.OriginalFileName)
	assert.Equal(t, "job-trailer", final.Movie.TrailerURL)
	assert.Equal(t, []string{"review-shot"}, final.Movie.Screenshots)

	persisted, err := repos.MovieRepo.FindByID(ctx, movieID)
	require.NoError(t, err)
	require.Len(t, persisted.Actresses, 1)
	require.Len(t, persisted.Credits, 1)
	assert.Equal(t, target.ID, persisted.Actresses[0].ID)
	assert.Equal(t, target.ID, persisted.Credits[0].ActressID)
	assert.Equal(t, "Canonical", persisted.Actresses[0].FirstName)
}

func TestApplyWritebackPreservesAdoptedCanonicalPR260(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()
	const filePath = "/source/PR260-ADOPT.mp4"
	const contentID = "pr260-adopt"
	const movieID = "PR260-ADOPT"

	source := models.Actress{FirstName: "Old", LastName: "Identity", Verified: false, Origin: "scrape"}
	require.NoError(t, db.Create(&source).Error)
	movie := models.Movie{ContentID: contentID, ID: movieID, Title: "Persisted title"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: contentID, ActressID: source.ID, CreditedName: "Adopted Canonical", Origin: "scrape"}
	require.NoError(t, db.Create(&credit).Error)
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{source}))
	collision := models.CreditCollision{
		CreditID:       credit.ID,
		MovieContentID: contentID,
		Field:          models.CreditFieldIdentityLink,
		ReportedValue:  "Adopted Canonical",
		CanonicalValue: "Old Identity",
		Status:         models.CollisionStatusOpen,
		Occurrences:    1,
	}
	require.NoError(t, db.Create(&collision).Error)
	_, err := database.NewCollisionService(db).Resolve(ctx, collision.ID, models.CollisionResolutionAdoptCanonical, 0)
	require.NoError(t, err)
	authoritative, err := repos.MovieRepo.FindByID(ctx, movieID)
	require.NoError(t, err)
	require.Len(t, authoritative.Actresses, 1)
	require.True(t, authoritative.Actresses[0].Verified)
	require.Equal(t, "Canonical", authoritative.Actresses[0].FirstName)

	old := &models.Movie{ID: movieID, ContentID: contentID, Actresses: []models.Actress{source}, Credits: []models.MovieCredit{{MovieContentID: contentID, ActressID: source.ID, CreditedName: "Adopted Canonical", Actress: &source}}}
	store := resultstore.New(1, []string{filePath})
	store.UpdateFileResult(filePath, &resultstore.MovieResult{ResultID: "pr260-adopt-result", Revision: 1, Status: models.JobStatusCompleted, Movie: old.Clone(), FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID}})
	phaseOut := old.Clone()
	phaseOut.Actresses = authoritative.Actresses
	phaseOut.Credits = authoritative.Credits
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: phaseOut}}
	inputs := makeApplyInputs(wf)
	inputs.Updater = store
	inputs.MovieRepo = repos.MovieRepo
	inputs.Results = map[string]*resultstore.MovieResult{filePath: {ResultID: "pr260-adopt-result", Revision: 1, Status: models.JobStatusCompleted, Movie: old.Clone(), FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID}}}
	NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{Destination: "/output"})

	final, err := store.GetMovieResult(filePath)
	require.NoError(t, err)
	require.Len(t, final.Movie.Actresses, 1)
	require.Len(t, final.Movie.Credits, 1)
	assert.Equal(t, authoritative.Actresses[0].ID, final.Movie.Actresses[0].ID)
	assert.True(t, final.Movie.Actresses[0].Verified)
	assert.Equal(t, "Canonical", final.Movie.Actresses[0].FirstName)
	assert.Equal(t, authoritative.Credits[0].ActressID, final.Movie.Credits[0].ActressID)
	persisted, err := repos.MovieRepo.FindByID(ctx, movieID)
	require.NoError(t, err)
	require.Len(t, persisted.Actresses, 1)
	require.Len(t, persisted.Credits, 1)
	assert.True(t, persisted.Actresses[0].Verified)
	assert.Equal(t, authoritative.Credits[0].ActressID, persisted.Credits[0].ActressID)
}

func TestApplyWritebackCollisionRace_DoesNotPublishStaleResultPR260(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()
	const filePath = "/source/PR260-race.mp4"
	const contentID = "pr260-race"
	const movieID = "PR260-RACE"

	source := models.Actress{FirstName: "Source", LastName: "Identity", Verified: true, Origin: "scrape"}
	target := models.Actress{FirstName: "Canonical", LastName: "Identity", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&target).Error)
	movie := models.Movie{ContentID: contentID, ID: movieID, Title: "Persisted title"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: contentID, ActressID: source.ID, CreditedName: "Source Identity", Origin: "scrape"}
	require.NoError(t, db.Create(&credit).Error)
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{source}))
	collision := models.CreditCollision{
		CreditID:       credit.ID,
		MovieContentID: contentID,
		Field:          models.CreditFieldCreditedName,
		ReportedValue:  "Source Identity",
		CanonicalValue: "Canonical Identity",
		Status:         models.CollisionStatusOpen,
		Occurrences:    1,
	}
	require.NoError(t, db.Create(&collision).Error)

	old := &models.Movie{
		ID:               movieID,
		ContentID:        contentID,
		Title:            "Stale apply title",
		OriginalFileName: "stale-file.mkv",
		Actresses:        []models.Actress{source},
		Credits: []models.MovieCredit{{
			MovieContentID: contentID,
			ActressID:      source.ID,
			CreditedName:   "Source Identity",
			Actress:        &source,
		}},
	}
	store := resultstore.New(1, []string{filePath})
	store.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID: "pr260-race-result",
		Revision: 1,
		Status:   models.JobStatusCompleted,
		Movie:    old.Clone(),
		FileMatchInfo: models.FileMatchInfo{
			Path:    filePath,
			MovieID: movieID,
		},
	})
	initial, err := repos.MovieRepo.FindByID(ctx, movieID)
	require.NoError(t, err)
	require.Len(t, initial.Actresses, 1)
	assert.Equal(t, source.ID, initial.Actresses[0].ID)
	assert.Zero(t, initial.RenderGeneration)

	entered := make(chan struct{})
	resume := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(resume)
		}
	}()
	wf := &pausedApplyWorkflow{
		stubApplyWorkflow: stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: old.Clone()}},
		entered:           entered,
		resume:            resume,
	}
	inputs := makeApplyInputs(wf)
	inputs.Updater = store
	inputs.MovieRepo = repos.MovieRepo
	inputs.Results = map[string]*resultstore.MovieResult{
		filePath: {
			ResultID: "pr260-race-result",
			Revision: 1,
			Status:   models.JobStatusCompleted,
			Movie:    old.Clone(),
			FileMatchInfo: models.FileMatchInfo{
				Path:    filePath,
				MovieID: movieID,
			},
		},
	}
	done := make(chan struct{})
	go func() {
		NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{Destination: "/output"})
		close(done)
	}()
	<-entered
	paused := wf.getLastCmd()
	require.NotNil(t, paused.Movie)
	require.Len(t, paused.Movie.Actresses, 1)
	assert.Equal(t, source.ID, paused.Movie.Actresses[0].ID)

	_, err = database.NewCollisionService(db).Resolve(ctx, collision.ID, models.CollisionResolutionReassign, target.ID)
	require.NoError(t, err)
	committed, err := repos.MovieRepo.FindByID(ctx, movieID)
	require.NoError(t, err)
	require.Len(t, committed.Actresses, 1)
	require.Len(t, committed.Credits, 1)
	assert.Equal(t, target.ID, committed.Actresses[0].ID)
	assert.Equal(t, target.ID, committed.Credits[0].ActressID)
	assert.True(t, committed.RenderDirty)
	assert.EqualValues(t, 1, committed.RenderGeneration)

	close(resume)
	released = true
	<-done

	final, err := store.GetMovieResult(filePath)
	require.NoError(t, err)
	require.NotNil(t, final.Movie)
	require.Len(t, final.Movie.Actresses, 1)
	require.Len(t, final.Movie.Credits, 1)
	assert.Equal(t, uint64(1), final.Revision)
	assert.Equal(t, source.ID, final.Movie.Actresses[0].ID)
	assert.Equal(t, source.ID, final.Movie.Credits[0].ActressID)
	persisted, err := repos.MovieRepo.FindByID(ctx, movieID)
	require.NoError(t, err)
	assert.True(t, persisted.RenderDirty)
	assert.EqualValues(t, 1, persisted.RenderGeneration)
}

func TestApplyPublicationFence_CleanupOnFailureAndCancellationPR260(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()
	const contentID = "pr260-fence-cleanup"
	movie := models.Movie{ContentID: contentID, ID: "PR260-FENCE-CLEANUP", Title: "Fence cleanup"}
	require.NoError(t, db.Create(&movie).Error)

	fencer, ok := repos.MovieRepo.(database.ApplyPublicationFencer)
	require.True(t, ok)
	callbackErr := errors.New("publication callback failed")
	err := fencer.WithApplyPublicationFence(ctx, contentID, 0, func(*models.Movie) error {
		return callbackErr
	})
	require.ErrorIs(t, err, callbackErr)
	require.NoError(t, db.Model(&models.Movie{}).Where("content_id = ?", contentID).Update("render_dirty", true).Error)

	cancelled, cancel := context.WithCancel(ctx)
	err = fencer.WithApplyPublicationFence(cancelled, contentID, 0, func(*models.Movie) error {
		cancel()
		return context.Canceled
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, db.Model(&models.Movie{}).Where("content_id = ?", contentID).Update("render_dirty", false).Error)

	persisted, err := repos.MovieRepo.FindByID(ctx, movie.ID)
	require.NoError(t, err)
	assert.False(t, persisted.RenderDirty)
	assert.Zero(t, persisted.RenderGeneration)
}

type pausedApplyWorkflow struct {
	stubApplyWorkflow
	entered chan struct{}
	resume  <-chan struct{}
}

func (s *pausedApplyWorkflow) Apply(_ context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	s.mu.Lock()
	s.applyCalled++
	s.lastCmd = cmd
	result := s.applyResult
	err := s.applyErr
	s.mu.Unlock()
	close(s.entered)
	<-s.resume
	return result, err
}
