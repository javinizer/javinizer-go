package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestBatchRefreshPrefersContentIdentityOverMatcherAlias(t *testing.T) {
	deps := createTestDeps(t, &config.Config{}, "")
	actress := models.Actress{FirstName: "Current", LastName: "Performer", Verified: true, Origin: "user"}
	require.NoError(t, deps.Repos.DB.Create(&actress).Error)
	movie := models.Movie{ContentID: "CONTENT-401", ID: "CANONICAL-401", Title: "Current"}
	require.NoError(t, deps.Repos.DB.Create(&movie).Error)
	require.NoError(t, deps.Repos.DB.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Current Credit"}).Error)
	require.NoError(t, deps.Repos.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actress.ID).Error)
	job := deps.JobStore.CreateJobBatch([]string{"part1.mp4", "part2.mp4"})
	for i, path := range []string{"part1.mp4", "part2.mp4"} {
		setJobResult(job, path, &resultstore.MovieResult{ResultID: path, Revision: uint64(i + 1), FileMatchInfo: models.FileMatchInfo{MovieID: "MATCHER-ALIAS", PartNumber: i + 1}, Movie: &models.Movie{ContentID: movie.ContentID, ID: movie.ID, Actresses: []models.Actress{{FirstName: "Stale"}}}})
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/batch/"+job.GetID()+"?include_data=true", nil)
	getBatchJobFull(deps, c, job.GetID())
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Current")
	require.NotContains(t, recorder.Body.String(), "Stale")
}

func TestDisplayTitlePreviewUsesAuthoritativeCreditsFromCreditlessDTO(t *testing.T) {
	cfg := newDisplayTitlePreviewConfig("<ACTRESSES>")
	cfg.Metadata.NFO.Feature.UseCreditedName = true
	deps := createTestDeps(t, cfg, "")
	actress := models.Actress{FirstName: "Canonical", LastName: "Name", Verified: true, Origin: "user"}
	require.NoError(t, deps.Repos.DB.Create(&actress).Error)
	movie := models.Movie{ContentID: "CONTENT-402", ID: "CANONICAL-402", Title: "Stored"}
	require.NoError(t, deps.Repos.DB.Create(&movie).Error)
	require.NoError(t, deps.Repos.DB.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Credited Alias", OrderIndex: 0}).Error)
	require.NoError(t, deps.Repos.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actress.ID).Error)
	job := deps.JobStore.CreateJobBatch([]string{"preview.mp4"})
	setJobResult(job, "preview.mp4", &resultstore.MovieResult{ResultID: "preview-result", FileMatchInfo: models.FileMatchInfo{MovieID: "MATCHER-ALIAS"}, Movie: &models.Movie{ContentID: movie.ContentID, ID: movie.ID, Title: "Snapshot"}})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.DisplayTitlePreviewRequest{Movie: &contracts.MovieView{Code: movie.ContentID, ID: movie.ID, Title: "Edited"}})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/preview-result/display-title-preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var response contracts.DisplayTitlePreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, "Credited Alias", response.DisplayTitle)
}

type authorityLookupRepo struct {
	database.MovieRepositoryInterface
	findByContentID func(context.Context, string) (*models.Movie, error)
	findByID        func(context.Context, string) (*models.Movie, error)
}

func (r authorityLookupRepo) FindByContentID(ctx context.Context, id string) (*models.Movie, error) {
	return r.findByContentID(ctx, id)
}

func (r authorityLookupRepo) FindByID(ctx context.Context, id string) (*models.Movie, error) {
	return r.findByID(ctx, id)
}

func TestAuthoritativeMovieLookupFallbacksAndErrors(t *testing.T) {
	ctx := context.Background()
	require.Nil(t, func() *models.Movie {
		movie, err := findAuthoritativeMovie(ctx, nil, &models.Movie{}, "")
		require.NoError(t, err)
		return movie
	}())
	require.Nil(t, func() *models.Movie {
		movie, err := findAuthoritativeMovie(ctx, authorityLookupRepo{}, nil, "")
		require.NoError(t, err)
		return movie
	}())

	boom := errors.New("read failed")
	repo := authorityLookupRepo{
		findByContentID: func(context.Context, string) (*models.Movie, error) { return nil, database.ErrNotFound },
		findByID: func(_ context.Context, id string) (*models.Movie, error) {
			switch id {
			case "canonical":
				return &models.Movie{ID: "canonical"}, nil
			case "missing":
				return nil, database.ErrNotFound
			default:
				return nil, boom
			}
		},
	}
	movie, err := findAuthoritativeMovie(ctx, repo, &models.Movie{ContentID: "content", ID: "canonical"}, "alias")
	require.NoError(t, err)
	require.Equal(t, "canonical", movie.ID)
	movie, err = findAuthoritativeMovie(ctx, repo, &models.Movie{ID: "missing"}, "missing")
	require.NoError(t, err)
	require.Nil(t, movie)
	_, err = findAuthoritativeMovie(ctx, repo, &models.Movie{ID: "broken"}, "alias")
	require.ErrorIs(t, err, boom)

	contentFailure := authorityLookupRepo{
		findByContentID: func(context.Context, string) (*models.Movie, error) { return nil, boom },
		findByID:        func(context.Context, string) (*models.Movie, error) { return nil, nil },
	}
	_, err = findAuthoritativeMovie(ctx, contentFailure, &models.Movie{ContentID: "content"}, "")
	require.ErrorIs(t, err, boom)

	aliasRepo := authorityLookupRepo{
		findByContentID: func(context.Context, string) (*models.Movie, error) { return nil, database.ErrNotFound },
		findByID: func(_ context.Context, id string) (*models.Movie, error) {
			if id == "alias" {
				return nil, database.ErrNotFound
			}
			return &models.Movie{ID: "other"}, nil
		},
	}
	movie, err = findAuthoritativeMovie(ctx, aliasRepo, &models.Movie{}, "alias")
	require.NoError(t, err)
	require.Nil(t, movie)
	movie, err = findAuthoritativeMovie(ctx, aliasRepo, &models.Movie{}, "other")
	require.NoError(t, err)
	require.Equal(t, "other", movie.ID)
	_, err = findAuthoritativeMovie(ctx, repo, &models.Movie{}, "boom")
	require.ErrorIs(t, err, boom)
}

func TestAuthoritativePreviewFallbackAndDisplayError(t *testing.T) {
	cfg := newDisplayTitlePreviewConfig("<TITLE>")
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"preview.mp4"})
	snapshot := &models.Movie{ContentID: "missing-content", ID: "missing-id", Title: "Snapshot", Credits: []models.MovieCredit{{CreditedName: "Authority"}}}
	setJobResult(job, "preview.mp4", &resultstore.MovieResult{ResultID: "result", Movie: snapshot})
	movie, resolveErr := authoritativePreviewMovie(context.Background(), deps, job.GetID(), "result", nil)
	require.Nil(t, resolveErr)
	require.NotSame(t, snapshot, movie)
	require.Equal(t, "Authority", movie.Credits[0].CreditedName)
	movie, resolveErr = authoritativePreviewMovie(context.Background(), deps, job.GetID(), "result", &contracts.MovieView{Title: "Edited"})
	require.Nil(t, resolveErr)
	require.Equal(t, snapshot.ID, movie.ID)
	require.Equal(t, snapshot.ContentID, movie.ContentID)

	_, resolveErr = authoritativePreviewMovie(context.Background(), deps, "missing-job", "result", nil)
	require.Equal(t, http.StatusNotFound, resolveErr.Status)
	_, resolveErr = authoritativePreviewMovie(context.Background(), deps, job.GetID(), "missing-result", nil)
	require.Equal(t, http.StatusNotFound, resolveErr.Status)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.DisplayTitlePreviewRequest{Movie: &contracts.MovieView{Title: "Edited"}})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/missing-job/results/result/display-title-preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}
