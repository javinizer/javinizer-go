package batch

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	actressapi "github.com/javinizer/javinizer-go/internal/api/actress"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestBatchMovieSaveDoesNotUndoCollisionRelinkWithStaleSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
	ctx := context.Background()
	db := deps.CoreDeps.DB
	repos := deps.Repos

	movie := models.Movie{ContentID: "PR260-STALE-SAVE", ID: "PR260-STALE-SAVE", Title: "Before"}
	require.NoError(t, db.DB.Create(&movie).Error)
	source := models.Actress{FirstName: "Source", LastName: "Legacy", Verified: true, Origin: "user"}
	target := models.Actress{FirstName: "Target", LastName: "Canonical", Verified: true, Origin: "user"}
	require.NoError(t, db.DB.Create(&source).Error)
	require.NoError(t, db.DB.Create(&target).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: source.FullName(), Origin: string(models.CreditOriginScrape)}
	require.NoError(t, db.DB.Create(&credit).Error)
	require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, source.ID).Error)
	collision := models.CreditCollision{
		MovieContentID: movie.ContentID,
		CreditID:       credit.ID,
		Field:          models.CreditFieldIdentityLink,
		ReportedValue:  source.FullName(),
		CanonicalValue: target.FullName(),
		Status:         models.CollisionStatusOpen,
	}
	require.NoError(t, db.DB.Create(&collision).Error)

	filePath := "/path/to/PR260-STALE-SAVE.mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID: "result-stale-relink",
		FileMatchInfo: models.FileMatchInfo{
			Path:    filePath,
			MovieID: movie.ID,
		},
		Status:    models.JobStatusCompleted,
		Movie:     &models.Movie{ContentID: movie.ContentID, ID: movie.ID, Title: movie.Title, Actresses: []models.Actress{source}},
		StartedAt: time.Now(),
	})

	router := gin.New()
	runtime := testkit.GetTestRuntime(deps)
	router.GET("/api/v1/batch/:id", getBatchJob(runtime))
	router.PATCH("/api/v1/batch/:id/results/:resultId", updateBatchMovie(runtime))
	actressapi.RegisterRoutes(router.Group("/api/v1"), actressapi.NewActressDeps(repos.ContentRepos, repos.TranslationRepos))

	getBatch := func() contracts.BatchJobResponse {
		t.Helper()
		response := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+job.GetID()+"?include_data=true", nil)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var batch contracts.BatchJobResponse
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &batch))
		return batch
	}

	before := getBatch()
	stale := before.Results[filePath].Movie
	require.NotNil(t, stale)
	require.Len(t, stale.Actresses, 1)
	require.NotEmpty(t, stale.CastVersion)
	revision := before.Results[filePath].Revision
	require.NotZero(t, revision)

	resolved := doJSON(t, router, http.MethodPost, "/api/v1/actresses/collisions/"+strconv.FormatUint(uint64(collision.ID), 10)+"/resolve", map[string]any{
		"resolution":        models.CollisionResolutionReassign,
		"target_actress_id": target.ID,
	})
	require.Equal(t, http.StatusOK, resolved.Code, resolved.Body.String())
	authoritative := getBatch()
	require.Equal(t, target.ID, authoritative.Results[filePath].Movie.Actresses[0].ID)
	require.Equal(t, revision, authoritative.Results[filePath].Revision)
	live, err := job.Results().GetMovieResult(filePath)
	require.NoError(t, err)
	require.Equal(t, revision, live.Revision)

	require.True(t, shouldPreserveCachedActresses(contracts.MovieViewToModel(stale), live.Movie))
	stale.Title = "After relink"
	rawMovie, err := json.Marshal(stale)
	require.NoError(t, err)
	var metadataOnly map[string]any
	require.NoError(t, json.Unmarshal(rawMovie, &metadataOnly))
	delete(metadataOnly, "actresses")
	response := doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+before.Results[filePath].ResultID, map[string]any{
		"movie":                    metadataOnly,
		"expected_result_revision": revision,
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	persisted, err := repos.MovieRepo.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, "After relink", persisted.Title)
	require.Len(t, persisted.Actresses, 1)
	require.Equal(t, target.ID, persisted.Actresses[0].ID)
	targetAfter, err := repos.ActressRepo.FindByID(ctx, target.ID)
	require.NoError(t, err)
	require.Equal(t, target.FullName(), targetAfter.FullName())
	credits, err := repos.MovieCreditRepo.ListByMovie(ctx, movie.ContentID)
	require.NoError(t, err)
	var targetCredit, sourceCredit *models.MovieCredit
	for i := range credits {
		switch credits[i].ActressID {
		case target.ID:
			targetCredit = &credits[i]
		case source.ID:
			sourceCredit = &credits[i]
		}
	}
	require.NotNil(t, targetCredit)
	require.False(t, targetCredit.Suppressed)
	require.Nil(t, sourceCredit)

	after := getBatch()
	require.Equal(t, revision+1, after.Results[filePath].Revision)
	require.Equal(t, "After relink", after.Results[filePath].Movie.Title)
	require.Len(t, after.Results[filePath].Movie.Actresses, 1)
	require.Equal(t, target.ID, after.Results[filePath].Movie.Actresses[0].ID)
}

func TestBatchMovieSaveRejectsExplicitStaleCastAfterCollisionRelink(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
	ctx := context.Background()
	db := deps.CoreDeps.DB
	repos := deps.Repos

	movie := models.Movie{ContentID: "PR260-STALE-CAST", ID: "PR260-STALE-CAST", Title: "Before"}
	require.NoError(t, db.DB.Create(&movie).Error)
	source := models.Actress{FirstName: "Source", LastName: "Legacy", Verified: true, Origin: "user"}
	target := models.Actress{FirstName: "Target", LastName: "Canonical", Verified: true, Origin: "user"}
	require.NoError(t, db.DB.Create(&source).Error)
	require.NoError(t, db.DB.Create(&target).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: source.FullName(), Origin: string(models.CreditOriginScrape)}
	require.NoError(t, db.DB.Create(&credit).Error)
	require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, source.ID).Error)
	collision := models.CreditCollision{MovieContentID: movie.ContentID, CreditID: credit.ID, Field: models.CreditFieldIdentityLink, ReportedValue: source.FullName(), CanonicalValue: target.FullName(), Status: models.CollisionStatusOpen}
	require.NoError(t, db.DB.Create(&collision).Error)

	filePath := "/path/to/PR260-STALE-CAST.mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	setJobResult(job, filePath, &resultstore.MovieResult{ResultID: "result-stale-cast", FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movie.ID}, Status: models.JobStatusCompleted, Movie: &models.Movie{ContentID: movie.ContentID, ID: movie.ID, Title: movie.Title, Actresses: []models.Actress{source}}, StartedAt: time.Now()})

	router := gin.New()
	runtime := testkit.GetTestRuntime(deps)
	router.GET("/api/v1/batch/:id", getBatchJob(runtime))
	router.PATCH("/api/v1/batch/:id/results/:resultId", updateBatchMovie(runtime))
	actressapi.RegisterRoutes(router.Group("/api/v1"), actressapi.NewActressDeps(repos.ContentRepos, repos.TranslationRepos))

	before := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+job.GetID()+"?include_data=true", nil)
	require.Equal(t, http.StatusOK, before.Code, before.Body.String())
	var batch contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(before.Body.Bytes(), &batch))
	stale := batch.Results[filePath].Movie
	require.NotNil(t, stale)
	revision := batch.Results[filePath].Revision
	stale.Actresses[0].FirstName = "Edited stale source"
	stale.UpdatedAt = time.Time{}
	stale.Actresses[0].UpdatedAt = time.Time{}
	stale.Title = "Stale title"

	resolved := doJSON(t, router, http.MethodPost, "/api/v1/actresses/collisions/"+strconv.FormatUint(uint64(collision.ID), 10)+"/resolve", map[string]any{"resolution": models.CollisionResolutionReassign, "target_actress_id": target.ID})
	require.Equal(t, http.StatusOK, resolved.Code, resolved.Body.String())
	ambiguous := *stale
	ambiguous.CastVersion = ""
	response := doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+batch.Results[filePath].ResultID, contracts.UpdateMovieRequest{Movie: &ambiguous, ExpectedResultRevision: &revision})
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "refresh")
	response = doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+batch.Results[filePath].ResultID, contracts.UpdateMovieRequest{Movie: stale, ExpectedResultRevision: &revision})
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())

	persisted, err := repos.MovieRepo.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Len(t, persisted.Actresses, 1)
	require.Equal(t, target.ID, persisted.Actresses[0].ID)
	require.Equal(t, "Before", persisted.Title)
	credits, err := repos.MovieCreditRepo.ListByMovie(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Len(t, credits, 1)
	require.Equal(t, target.ID, credits[0].ActressID)

	freshResponse := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+job.GetID()+"?include_data=true", nil)
	require.Equal(t, http.StatusOK, freshResponse.Code, freshResponse.Body.String())
	var freshBatch contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(freshResponse.Body.Bytes(), &freshBatch))
	freshView := freshBatch.Results[filePath].Movie
	freshView.Actresses[0].FirstName = "Fresh intentional"
	freshView.Title = "Fresh title"
	response = doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+batch.Results[filePath].ResultID, contracts.UpdateMovieRequest{Movie: freshView, ExpectedResultRevision: &revision})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	persisted, err = repos.MovieRepo.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, "Fresh title", persisted.Title)
	require.Len(t, persisted.Actresses, 1)
	require.Equal(t, target.ID, persisted.Actresses[0].ID)
	require.Equal(t, "Fresh intentional", persisted.Actresses[0].FirstName)
}

func TestBatchMovieSaveRejectsExplicitStaleCastAfterCanonicalAdoption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
	ctx := context.Background()
	db := deps.CoreDeps.DB
	repos := deps.Repos

	movie := models.Movie{ContentID: "PR260-STALE-ADOPT", ID: "PR260-STALE-ADOPT", Title: "Before"}
	require.NoError(t, db.DB.Create(&movie).Error)
	source := models.Actress{FirstName: "Old", LastName: "Identity", Verified: false, Origin: "scrape"}
	require.NoError(t, db.DB.Create(&source).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: "Adopted Canonical", Origin: string(models.CreditOriginScrape)}
	require.NoError(t, db.DB.Create(&credit).Error)
	require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, source.ID).Error)
	collision := models.CreditCollision{MovieContentID: movie.ContentID, CreditID: credit.ID, Field: models.CreditFieldIdentityLink, ReportedValue: credit.CreditedName, CanonicalValue: source.FullName(), Status: models.CollisionStatusOpen}
	require.NoError(t, db.DB.Create(&collision).Error)

	filePath := "/path/to/PR260-STALE-ADOPT.mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	setJobResult(job, filePath, &resultstore.MovieResult{ResultID: "result-stale-adopt", FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movie.ID}, Status: models.JobStatusCompleted, Movie: &models.Movie{ContentID: movie.ContentID, ID: movie.ID, Title: movie.Title, Actresses: []models.Actress{source}}, StartedAt: time.Now()})

	router := gin.New()
	runtime := testkit.GetTestRuntime(deps)
	router.GET("/api/v1/batch/:id", getBatchJob(runtime))
	router.PATCH("/api/v1/batch/:id/results/:resultId", updateBatchMovie(runtime))
	actressapi.RegisterRoutes(router.Group("/api/v1"), actressapi.NewActressDeps(repos.ContentRepos, repos.TranslationRepos))

	before := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+job.GetID()+"?include_data=true", nil)
	require.Equal(t, http.StatusOK, before.Code, before.Body.String())
	var batch contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(before.Body.Bytes(), &batch))
	stale := batch.Results[filePath].Movie
	require.NotNil(t, stale)
	revision := batch.Results[filePath].Revision
	stale.Actresses[0].FirstName = "Edited stale canonical"
	stale.UpdatedAt = time.Time{}
	stale.Actresses[0].UpdatedAt = time.Time{}
	stale.Title = "Stale title"

	resolved := doJSON(t, router, http.MethodPost, "/api/v1/actresses/collisions/"+strconv.FormatUint(uint64(collision.ID), 10)+"/resolve", map[string]any{"resolution": models.CollisionResolutionAdoptCanonical})
	require.Equal(t, http.StatusOK, resolved.Code, resolved.Body.String())
	authoritative, err := repos.ActressRepo.FindByID(ctx, source.ID)
	require.NoError(t, err)
	require.NotEqual(t, "Edited stale canonical", authoritative.FirstName)
	response := doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+batch.Results[filePath].ResultID, contracts.UpdateMovieRequest{Movie: stale})
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "refresh")
	response = doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+batch.Results[filePath].ResultID, contracts.UpdateMovieRequest{Movie: stale, ExpectedResultRevision: &revision})
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())

	persisted, err := repos.MovieRepo.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Len(t, persisted.Actresses, 1)
	require.Equal(t, source.ID, persisted.Actresses[0].ID)
	require.Equal(t, authoritative.FirstName, persisted.Actresses[0].FirstName)
	require.Equal(t, "Before", persisted.Title)

	freshResponse := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+job.GetID()+"?include_data=true", nil)
	require.Equal(t, http.StatusOK, freshResponse.Code, freshResponse.Body.String())
	var freshBatch contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(freshResponse.Body.Bytes(), &freshBatch))
	freshView := freshBatch.Results[filePath].Movie
	freshView.Actresses[0].FirstName = "Fresh canonical edit"
	freshView.Title = "Fresh title"
	response = doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+batch.Results[filePath].ResultID, contracts.UpdateMovieRequest{Movie: freshView, ExpectedResultRevision: &revision})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	persisted, err = repos.MovieRepo.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, "Fresh canonical edit", persisted.Actresses[0].FirstName)
	require.Equal(t, "Fresh title", persisted.Title)
}

func TestBatchExplicitCastDeletedPersistedMovieConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
	ctx := context.Background()
	movie := models.Movie{ContentID: "PR260-FCA02", ID: "PR260-FCA02", Title: "Before"}
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&movie).Error)
	actress := models.Actress{FirstName: "Original", LastName: "Cast", Verified: true, Origin: "user"}
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&actress).Error)
	require.NoError(t, deps.CoreDeps.DB.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actress.ID).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: actress.FullName(), Origin: string(models.CreditOriginScrape)}
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&credit).Error)
	path := "/path/to/PR260-FCA02.mp4"
	job := deps.JobStore.CreateJobBatch([]string{path})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	setJobResult(job, path, &resultstore.MovieResult{ResultID: "result-fca02", FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}, Status: models.JobStatusCompleted, Movie: &models.Movie{ContentID: movie.ContentID, ID: movie.ID, Title: movie.Title, Actresses: []models.Actress{actress}}, StartedAt: time.Now()})
	router := gin.New()
	runtime := testkit.GetTestRuntime(deps)
	router.GET("/api/v1/batch/:id", getBatchJob(runtime))
	router.PATCH("/api/v1/batch/:id/results/:resultId", updateBatchMovie(runtime))
	beforeResponse := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+job.GetID()+"?include_data=true", nil)
	require.Equal(t, http.StatusOK, beforeResponse.Code, beforeResponse.Body.String())
	var before contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(beforeResponse.Body.Bytes(), &before))
	require.Contains(t, before.Results, path, beforeResponse.Body.String())
	require.NotNil(t, before.Results[path].Movie, beforeResponse.Body.String())
	view := before.Results[path].Movie
	require.NotEmpty(t, view.CastVersion)
	revision := before.Results[path].Revision
	require.NotZero(t, revision)
	require.NoError(t, deps.CoreDeps.DB.DB.Delete(&movie).Error)
	var baselineCredits, baselineJoins int64
	require.NoError(t, deps.CoreDeps.DB.DB.Model(&models.MovieCredit{}).Where("movie_content_id = ?", movie.ContentID).Count(&baselineCredits).Error)
	require.NoError(t, deps.CoreDeps.DB.DB.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Count(&baselineJoins).Error)
	view.Actresses[0].FirstName = "Resurrected"
	response := doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/"+before.Results[path].ResultID, contracts.UpdateMovieRequest{Movie: view, ExpectedResultRevision: &revision})
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "refresh")
	_, err := deps.Repos.MovieRepo.FindByContentID(ctx, movie.ContentID)
	require.Error(t, err)
	var creditCount, joinCount int64
	require.NoError(t, deps.CoreDeps.DB.DB.Model(&models.MovieCredit{}).Where("movie_content_id = ?", movie.ContentID).Count(&creditCount).Error)
	require.Equal(t, baselineCredits, creditCount)
	require.NoError(t, deps.CoreDeps.DB.DB.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Count(&joinCount).Error)
	require.Equal(t, baselineJoins, joinCount)
	original, err := deps.Repos.ActressRepo.FindByID(ctx, actress.ID)
	require.NoError(t, err)
	require.Equal(t, "Original", original.FirstName)
	after, err := job.Results().GetMovieResult(path)
	require.NoError(t, err)
	require.Equal(t, revision, after.Revision)
	require.Equal(t, "Original", after.Movie.Actresses[0].FirstName)

	legacyID := "PR260-FCA02-LEGACY"
	legacyPath := "/path/to/PR260-FCA02-LEGACY.mp4"
	legacy := deps.JobStore.CreateJobBatch([]string{legacyPath})
	legacy.Controller().SetJobStatus(models.JobStatusCompleted)
	setJobResult(legacy, legacyPath, &resultstore.MovieResult{ResultID: "result-fca02-legacy", FileMatchInfo: models.FileMatchInfo{Path: legacyPath, MovieID: legacyID}, Status: models.JobStatusCompleted, Movie: &models.Movie{ContentID: legacyID, ID: legacyID, Title: "Legacy", Actresses: []models.Actress{{FirstName: "Legacy", LastName: "Cast"}}}, StartedAt: time.Now()})
	legacyGET := doJSON(t, router, http.MethodGet, "/api/v1/batch/"+legacy.GetID()+"?include_data=true", nil)
	require.Equal(t, http.StatusOK, legacyGET.Code, legacyGET.Body.String())
	var legacyBatch contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(legacyGET.Body.Bytes(), &legacyBatch))
	legacyView := legacyBatch.Results[legacyPath].Movie
	require.NotNil(t, legacyView)
	require.NotEmpty(t, legacyView.CastVersion)
	legacyRev := legacyBatch.Results[legacyPath].Revision
	legacyView.Actresses[0].FirstName = "Intentional"
	legacyPATCH := doJSON(t, router, http.MethodPatch, "/api/v1/batch/"+legacy.GetID()+"/results/result-fca02-legacy", contracts.UpdateMovieRequest{Movie: legacyView, ExpectedResultRevision: &legacyRev})
	require.Equal(t, http.StatusOK, legacyPATCH.Code, legacyPATCH.Body.String())
	created, err := deps.Repos.MovieRepo.FindByContentID(ctx, legacyID)
	require.NoError(t, err)
	require.Equal(t, "Intentional", created.Actresses[0].FirstName)
}
