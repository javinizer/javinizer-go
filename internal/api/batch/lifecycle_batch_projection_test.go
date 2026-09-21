package batch

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

type failingProjectionRepo struct{ err error }

func (r failingProjectionRepo) FindAuthoritativeProjections(context.Context, []string, []string) (*database.AuthoritativeMovieProjection, error) {
	return nil, r.err
}

func TestBatchProjectionPrecedenceAndDeepIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	ctx := context.Background()
	identity := models.Actress{JapaneseName: "Authoritative", Verified: true, Origin: "user", Aliases: "One|Two"}
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&identity).Error)
	movie := models.Movie{ContentID: "authority-content", ID: "AUTHORITY-ID", Title: "persisted", Screenshots: []string{"repo-media"}}
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: identity.ID, CreditedName: "Authority Credit"}
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&credit).Error)
	other := models.Movie{ContentID: "other-content", ID: "SNAPSHOT-ID", Title: "wrong canonical"}
	require.NoError(t, deps.CoreDeps.DB.DB.Create(&other).Error)

	job := deps.JobStore.CreateJobBatch([]string{"part1.mp4", "part2.mp4"})
	shared := &models.Movie{ContentID: movie.ContentID, ID: other.ID, Title: "snapshot", Screenshots: []string{"snapshot-media"}, Translations: []models.MovieTranslation{{Language: "en", Title: "snapshot translation"}}, Credits: []models.MovieCredit{{CreditedName: "stale"}}}
	setJobResult(job, "part1.mp4", &resultstore.MovieResult{ResultID: "part1", Movie: shared, FileMatchInfo: models.FileMatchInfo{MovieID: "missing-alias"}})
	setJobResult(job, "part2.mp4", &resultstore.MovieResult{ResultID: "part2", Movie: shared, FileMatchInfo: models.FileMatchInfo{MovieID: "missing-alias"}})
	status := job.GetStatus()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/batch?include_data=true", nil)
	require.NoError(t, refreshBatchJobMovies(c, deps, status))

	first := status.Results["part1.mp4"].Movie
	second := status.Results["part2.mp4"].Movie
	require.Equal(t, "Authority Credit", first.Credits[0].CreditedName)
	require.Equal(t, identity.ID, first.Actresses[0].ID)
	require.Equal(t, movie.ContentID, first.ContentID)
	first.Credits[0].CreditedName = "mutated"
	first.Credits[0].Actress.Aliases = "mutated aliases"
	first.Actresses[0].JapaneseName = "mutated actress"
	first.Screenshots[0] = "mutated media"
	first.Translations[0].Title = "mutated translation"
	require.Equal(t, "Authority Credit", second.Credits[0].CreditedName)
	require.Equal(t, "One|Two", second.Credits[0].Actress.Aliases)
	require.Equal(t, "Authoritative", second.Actresses[0].JapaneseName)
	require.Equal(t, "snapshot-media", second.Screenshots[0])
	require.Equal(t, "snapshot translation", second.Translations[0].Title)
	live := job.GetStatus()
	require.Equal(t, "stale", live.Results["part1.mp4"].Movie.Credits[0].CreditedName)
	projection, err := deps.Repos.MovieProjectionRepo.FindAuthoritativeProjections(ctx, []string{movie.ContentID}, nil)
	require.NoError(t, err)
	require.Equal(t, "Authority Credit", projection.ByContentID[movie.ContentID].Credits[0].CreditedName)
	require.Equal(t, "One|Two", projection.ByContentID[movie.ContentID].Credits[0].Actress.Aliases)
}

func TestBatchProjectionErrorLeavesEverySnapshotUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	boom := errors.New("batch projection failed")
	deps.Repos.MovieProjectionRepo = failingProjectionRepo{err: boom}
	firstMovie := &models.Movie{ContentID: "one", Credits: []models.MovieCredit{{CreditedName: "one"}}}
	secondMovie := &models.Movie{ContentID: "two", Credits: []models.MovieCredit{{CreditedName: "two"}}}
	first := &resultstore.MovieResult{Movie: firstMovie}
	second := &resultstore.MovieResult{Movie: secondMovie}
	status := &worker.BatchJobStatus{Results: map[string]*resultstore.MovieResult{"one": first, "two": second}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/batch?include_data=true", nil)
	require.ErrorIs(t, refreshBatchJobMovies(c, deps, status), boom)
	require.Same(t, first, status.Results["one"])
	require.Same(t, second, status.Results["two"])
	require.Same(t, firstMovie, status.Results["one"].Movie)
	require.Same(t, secondMovie, status.Results["two"].Movie)
}

func TestFindAuthoritativeMovieProjectionPrecedence(t *testing.T) {
	require.Nil(t, findAuthoritativeMovieProjection(nil, &models.Movie{}, ""))
	require.Nil(t, findAuthoritativeMovieProjection(&database.AuthoritativeMovieProjection{}, nil, ""))
	content := &models.Movie{ContentID: "content", ID: "CONTENT"}
	canonical := &models.Movie{ContentID: "canonical-content", ID: "canonical"}
	alias := &models.Movie{ContentID: "alias-content", ID: "alias"}
	projection := &database.AuthoritativeMovieProjection{ByContentID: map[string]*models.Movie{"content": content}, ByCanonicalID: map[string]*models.Movie{"canonical": canonical, "alias": alias}}
	require.Same(t, content, findAuthoritativeMovieProjection(projection, &models.Movie{ContentID: "content", ID: "canonical"}, "alias"))
	require.Same(t, canonical, findAuthoritativeMovieProjection(projection, &models.Movie{ID: "canonical"}, "alias"))
	require.Same(t, alias, findAuthoritativeMovieProjection(projection, &models.Movie{ID: "missing"}, "alias"))
	require.Nil(t, findAuthoritativeMovieProjection(projection, &models.Movie{ID: "missing"}, "missing"))
}
