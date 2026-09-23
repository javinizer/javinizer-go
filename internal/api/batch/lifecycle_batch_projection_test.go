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

type recordingProjectionRepo struct {
	calls        int
	contentIDs   []string
	canonicalIDs []string
}

func (r *recordingProjectionRepo) FindAuthoritativeProjections(_ context.Context, contentIDs, canonicalIDs []string) (*database.AuthoritativeMovieProjection, error) {
	r.calls++
	r.contentIDs = append([]string(nil), contentIDs...)
	r.canonicalIDs = append([]string(nil), canonicalIDs...)
	return &database.AuthoritativeMovieProjection{}, nil
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

func TestBatchProjectionDuplicateCanonicalIDFailsClosedInMixedRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	actresses := []models.Actress{{JapaneseName: "Duplicate A", Verified: true}, {JapaneseName: "Duplicate B", Verified: true}, {JapaneseName: "Unique", Verified: true}}
	require.NoError(t, deps.Repos.DB.Create(&actresses).Error)
	movies := []models.Movie{
		{ContentID: "duplicate-a", ID: "DUPLICATE"},
		{ContentID: "duplicate-b", ID: "DUPLICATE"},
		{ContentID: "unique-content", ID: "UNIQUE"},
	}
	require.NoError(t, deps.Repos.DB.Create(&movies).Error)
	require.NoError(t, deps.Repos.DB.Create(&[]models.MovieCredit{
		{MovieContentID: "duplicate-a", ActressID: actresses[0].ID, CreditedName: "Wrong if selected"},
		{MovieContentID: "duplicate-b", ActressID: actresses[1].ID, CreditedName: "Also wrong"},
		{MovieContentID: "unique-content", ActressID: actresses[2].ID, CreditedName: "Unique credit"},
	}).Error)

	job := deps.JobStore.CreateJobBatch([]string{"ambiguous.mp4", "exact.mp4", "unique.mp4"})
	ambiguous := &resultstore.MovieResult{ResultID: "ambiguous", FileMatchInfo: models.FileMatchInfo{MovieID: "UNIQUE"}, Movie: &models.Movie{ID: "DUPLICATE"}}
	setJobResult(job, "ambiguous.mp4", ambiguous)
	setJobResult(job, "exact.mp4", &resultstore.MovieResult{ResultID: "exact", Movie: &models.Movie{ContentID: "duplicate-b", ID: "DUPLICATE"}})
	setJobResult(job, "unique.mp4", &resultstore.MovieResult{ResultID: "unique", Movie: &models.Movie{ID: "UNIQUE"}})
	status := job.GetStatus()
	ambiguousSnapshot := status.Results["ambiguous.mp4"]
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/batch?include_data=true", nil)
	require.NoError(t, refreshBatchJobMovies(c, deps, status))

	require.Same(t, ambiguousSnapshot, status.Results["ambiguous.mp4"])
	require.Empty(t, status.Results["ambiguous.mp4"].Movie.Credits)
	require.Empty(t, status.Results["ambiguous.mp4"].Movie.Actresses)
	require.Equal(t, "Also wrong", status.Results["exact.mp4"].Movie.Credits[0].CreditedName)
	require.Equal(t, "Unique credit", status.Results["unique.mp4"].Movie.Credits[0].CreditedName)
}

func TestBatchProjectionCollectorUsesOneRequestForBothAliasDimensions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	repo := &recordingProjectionRepo{}
	deps.Repos.MovieProjectionRepo = repo
	job := &worker.BatchJobStatus{Results: map[string]*resultstore.MovieResult{
		"one": {Movie: &models.Movie{ContentID: "content-one", ID: "canonical-one"}, FileMatchInfo: models.FileMatchInfo{MovieID: "alias-one"}},
		"two": {Movie: &models.Movie{ContentID: "content-two", ID: "canonical-two"}, FileMatchInfo: models.FileMatchInfo{MovieID: "alias-two"}},
	}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/batch?include_data=true", nil)
	require.NoError(t, refreshBatchJobMovies(c, deps, job))
	require.Equal(t, 1, repo.calls)
	require.ElementsMatch(t, []string{"content-one", "alias-one", "content-two", "alias-two"}, repo.contentIDs)
	require.ElementsMatch(t, []string{"canonical-one", "alias-one", "canonical-two", "alias-two"}, repo.canonicalIDs)
}

func TestBatchProjectionAliasContentFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	actress := models.Actress{JapaneseName: "Alias Authority", Verified: true}
	require.NoError(t, deps.Repos.DB.Create(&actress).Error)
	movie := models.Movie{ContentID: "legacy-content-alias", ID: "CANONICAL-ALIAS", Title: "Persisted"}
	require.NoError(t, deps.Repos.DB.Create(&movie).Error)
	require.NoError(t, deps.Repos.DB.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Alias Credit"}).Error)

	job := deps.JobStore.CreateJobBatch([]string{"alias.mp4"})
	setJobResult(job, "alias.mp4", &resultstore.MovieResult{ResultID: "alias", FileMatchInfo: models.FileMatchInfo{MovieID: movie.ContentID}, Movie: &models.Movie{ID: "missing-snapshot-id", Credits: []models.MovieCredit{{CreditedName: "Stale"}}}})
	status := job.GetStatus()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/batch?include_data=true", nil)
	require.NoError(t, refreshBatchJobMovies(c, deps, status))
	require.Equal(t, "Alias Credit", status.Results["alias.mp4"].Movie.Credits[0].CreditedName)
	require.Equal(t, "Alias Authority", status.Results["alias.mp4"].Movie.Actresses[0].JapaneseName)
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
	contentIDs, canonicalIDs := movieProjectionLookupIDs(nil, "alias")
	require.Nil(t, contentIDs)
	require.Nil(t, canonicalIDs)
	movie, canonicalAmbiguous := projectionMovieByCanonicalID(nil, "")
	require.Nil(t, movie)
	require.False(t, canonicalAmbiguous)
	require.Nil(t, projectionMovieByContentID(nil, "alias"))
	require.Nil(t, projectionMovieByContentID(&database.AuthoritativeMovieProjection{}, "alias"))
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

	collisionCanonical := &models.Movie{ContentID: "canonical-content", ID: "collision"}
	collisionContent := &models.Movie{ContentID: "collision", ID: "other"}
	collision := &database.AuthoritativeMovieProjection{
		ByContentID:   map[string]*models.Movie{"collision": collisionContent},
		ByCanonicalID: map[string]*models.Movie{"collision": collisionCanonical},
	}
	require.Same(t, collisionCanonical, findAuthoritativeMovieProjection(collision, &models.Movie{ID: "missing"}, "collision"), "alias canonical ID must retain FindByID precedence over content ID")

	ambiguous := &database.AuthoritativeMovieProjection{
		ByContentID:           map[string]*models.Movie{"alias": {ContentID: "alias", ID: "safe-alias"}},
		ByCanonicalID:         map[string]*models.Movie{"alias": {ContentID: "safe-alias", ID: "alias"}},
		AmbiguousCanonicalIDs: map[string]struct{}{"duplicate": {}},
	}
	require.Nil(t, findAuthoritativeMovieProjection(ambiguous, &models.Movie{ID: "duplicate"}, "alias"), "an ambiguous snapshot canonical ID must fail closed before alias fallback")
	ambiguous.AmbiguousCanonicalIDs["alias"] = struct{}{}
	require.Nil(t, findAuthoritativeMovieProjection(ambiguous, &models.Movie{ID: "missing"}, "alias"), "an ambiguous alias canonical ID must fail closed before content fallback")
	require.Nil(t, findAuthoritativeMovieProjection(&database.AuthoritativeMovieProjection{ByContentID: map[string]*models.Movie{"content": {ContentID: "wrong"}}}, &models.Movie{ContentID: "content"}, ""))
	require.Nil(t, findAuthoritativeMovieProjection(&database.AuthoritativeMovieProjection{ByCanonicalID: map[string]*models.Movie{"alias": {ID: "wrong"}}}, &models.Movie{}, "alias"))
}
