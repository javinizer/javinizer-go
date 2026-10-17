package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	actressapi "github.com/javinizer/javinizer-go/internal/api/actress"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// Decode the wire DTO directly. BatchFileResult's persistence compatibility
// decoder consumes content_id instead of code and would lose this identity.
type suppressionWireBatch struct {
	Results map[string]struct {
		Movie    *contracts.MovieView `json:"movie"`
		Revision uint64               `json:"revision"`
		ResultID string               `json:"result_id"`
	} `json:"results"`
}

type suppressionFixture struct {
	deps    *core.APIDeps
	router  *gin.Engine
	job     *worker.BatchJob
	movie   models.Movie
	actress models.Actress
	credit  models.MovieCredit
	paths   []string
}

func newSuppressionFixture(t *testing.T, parts int) *suppressionFixture {
	t.Helper()
	return newSuppressionFixtureWithConfig(t, parts, config.DefaultConfig(nil, nil))
}

func newSuppressionFixtureWithConfig(t *testing.T, parts int, cfg *config.Config) *suppressionFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg.System.TempDir = t.TempDir()
	f := &suppressionFixture{deps: createTestDeps(t, cfg, "")}
	f.actress = models.Actress{FirstName: "Canonical", LastName: "Name", Verified: true, Origin: "user"}
	require.NoError(t, f.deps.Repos.DB.Create(&f.actress).Error)
	f.movie = models.Movie{ID: "SUPPRESS-API", ContentID: "suppress-api", Title: "Before", DisplayTitle: "Before"}
	require.NoError(t, f.deps.Repos.DB.Create(&f.movie).Error)
	require.NoError(t, f.deps.Repos.DB.Exec("INSERT INTO movie_actresses(movie_content_id,actress_id) VALUES(?,?)", f.movie.ContentID, f.actress.ID).Error)
	f.credit = models.MovieCredit{MovieContentID: f.movie.ContentID, ActressID: f.actress.ID, CreditedName: "Reported", Origin: "user", OverrideName: "Saved override", UserOverride: true, OrderPinned: true, OrderIndex: 3}
	require.NoError(t, f.deps.Repos.DB.Create(&f.credit).Error)
	before, err := f.deps.Repos.MovieRepo.FindByContentID(t.Context(), f.movie.ContentID)
	require.NoError(t, err)
	for i := 0; i < parts; i++ {
		f.paths = append(f.paths, fmt.Sprintf("synthetic-%d.mp4", i))
	}
	f.job = f.deps.JobStore.CreateJobBatch(f.paths)
	f.job.Controller().SetJobStatus(models.JobStatusCompleted)
	for i, p := range f.paths {
		setJobResult(f.job, p, &resultstore.MovieResult{ResultID: fmt.Sprintf("suppression-result-%d", i), Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: f.movie.ID}, Movie: before.Clone(), PersistedMovie: true})
	}
	rt := testkit.GetTestRuntime(f.deps)
	f.router = gin.New()
	f.router.GET("/batch/:id", getBatchJob(rt))
	f.router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(rt))
	actressapi.RegisterRoutes(f.router.Group("/"), actressapi.NewActressDeps(f.deps.Repos.ContentRepos, f.deps.Repos.TranslationRepos))
	return f
}

func (f *suppressionFixture) read(t *testing.T) suppressionWireBatch {
	t.Helper()
	r := doJSON(t, f.router, http.MethodGet, "/batch/"+f.job.GetID()+"?include_data=true", nil)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	var out suppressionWireBatch
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &out))
	require.Equal(t, f.movie.ContentID, out.Results[f.paths[0]].Movie.Code)
	return out
}

func (f *suppressionFixture) suppress(t *testing.T) {
	t.Helper()
	r := doJSON(t, f.router, http.MethodPost, fmt.Sprintf("/actresses/credits/%d/suppress", f.credit.ID), map[string]bool{"suppressed": true})
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
}

func (f *suppressionFixture) patch(t *testing.T, req contracts.UpdateMovieRequest, want int) *contracts.MovieResponse {
	t.Helper()
	r := doJSON(t, f.router, http.MethodPatch, "/batch/"+f.job.GetID()+"/results/suppression-result-0", req)
	require.Equal(t, want, r.Code, r.Body.String())
	if want != http.StatusOK {
		return nil
	}
	var out contracts.MovieResponse
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &out))
	return &out
}

func (f *suppressionFixture) stored(t *testing.T) *models.Movie {
	t.Helper()
	m, err := f.deps.Repos.MovieRepo.FindByContentID(t.Context(), f.movie.ContentID)
	require.NoError(t, err)
	return m
}

func TestCreditSuppressionMetadataEcho(t *testing.T) {
	for _, code := range []string{"wire", "omitted"} {
		for _, cast := range []string{"stale", "current", "nil"} {
			for _, revision := range []string{"omitted", "current", "stale"} {
				t.Run(code+"/"+cast+"/"+revision, func(t *testing.T) {
					f := newSuppressionFixture(t, 1)
					old := f.read(t)
					require.Len(t, old.Results[f.paths[0]].Movie.Actresses, 1)
					f.suppress(t)
					current := f.read(t)
					require.Empty(t, current.Results[f.paths[0]].Movie.Actresses)
					require.Equal(t, uint64(1), current.Results[f.paths[0]].Revision, "credit API does not increment result revisions")
					require.Equal(t, int64(1), f.stored(t).RenderGeneration)
					view := old.Results[f.paths[0]].Movie
					if cast == "current" {
						view = current.Results[f.paths[0]].Movie
					}
					if cast == "nil" {
						view.Actresses = nil
					}
					if code == "omitted" {
						view.Code = ""
					}
					view.Title = "After"
					// Read-only credit fields are never accepted as edits.
					view.Credits = []contracts.MovieCreditView{{ID: f.credit.ID, ActressID: f.actress.ID}}
					view.Credits[0].Suppressed = false
					view.Credits[0].OverrideName = "Forged override"
					view.Credits[0].UserOverride = false
					req := contracts.UpdateMovieRequest{Movie: view}
					want := http.StatusOK
					if cast == "current" && revision == "omitted" {
						want = http.StatusConflict
					}
					if revision != "omitted" {
						rev := current.Results[f.paths[0]].Revision
						if revision == "stale" {
							rev--
							want = http.StatusConflict
						}
						req.ExpectedResultRevision = &rev
					}
					response := f.patch(t, req, want)
					saved := f.stored(t)
					require.Len(t, saved.Credits, 1)
					require.True(t, saved.Credits[0].Suppressed)
					require.True(t, saved.Credits[0].UserOverride)
					require.Equal(t, "Saved override", saved.Credits[0].OverrideName)
					require.Equal(t, 3, saved.Credits[0].OrderIndex)
					require.Empty(t, saved.Actresses)
					live, err := f.job.Results().GetMovieResult(f.paths[0])
					require.NoError(t, err)
					if want == http.StatusOK {
						require.Equal(t, "After", saved.Title)
						require.Equal(t, int64(2), saved.RenderGeneration)
						require.Equal(t, uint64(2), live.Revision)
						require.Empty(t, live.Movie.Actresses)
						require.Empty(t, response.Movie.Actresses)
						require.Equal(t, f.movie.ContentID, response.Movie.Code)
					} else {
						require.Equal(t, "Before", saved.Title)
						require.Equal(t, int64(1), saved.RenderGeneration)
						require.Equal(t, uint64(1), live.Revision)
					}
				})
			}
		}
	}
}

func TestCreditSuppressionExplicitCastEdits(t *testing.T) {
	for _, action := range []string{"add", "remove", "rename", "restore"} {
		for _, stale := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stale=%v", action, stale), func(t *testing.T) {
				f := newSuppressionFixture(t, 1)
				old := f.read(t)
				f.suppress(t)
				fresh := f.read(t)
				view := fresh.Results[f.paths[0]].Movie
				if stale {
					view = old.Results[f.paths[0]].Movie
				}
				second := models.Actress{FirstName: "Second", LastName: "Person", Verified: true, Origin: "user"}
				require.NoError(t, f.deps.Repos.DB.Create(&second).Error)
				switch action {
				case "add":
					view.Actresses = append(view.Actresses, *contracts.ActressViewFromModel(&second))
				case "remove":
					view.Actresses = []contracts.ActressView{}
				case "rename":
					view.Actresses = []contracts.ActressView{*contracts.ActressViewFromModel(&f.actress)}
					view.Actresses[0].FirstName = "Intentional rename"
				case "restore":
					view.Actresses = []contracts.ActressView{*contracts.ActressViewFromModel(&f.actress)}
				}
				view.Title = "Explicit"
				rev := uint64(1)
				want := http.StatusOK
				// An unchanged old echo is deliberately preserved. An explicit removal
				// carries the old cast version and conflicts after concurrent suppression.
				if stale && action != "restore" {
					want = http.StatusConflict
				}
				f.patch(t, contracts.UpdateMovieRequest{Movie: view, ExpectedResultRevision: &rev}, want)
				saved := f.stored(t)
				if want == http.StatusConflict {
					require.Equal(t, "Before", saved.Title)
					require.True(t, saved.Credits[0].Suppressed)
					return
				}
				require.Equal(t, "Explicit", saved.Title)
				switch action {
				case "add":
					require.Len(t, saved.Actresses, 1)
					require.Equal(t, second.ID, saved.Actresses[0].ID)
				case "remove":
					require.Empty(t, saved.Actresses)
				case "rename":
					require.Len(t, saved.Actresses, 1)
					require.Equal(t, "Intentional rename", saved.Actresses[0].FirstName)
				case "restore":
					if stale {
						require.Empty(t, saved.Actresses)
					} else {
						require.Len(t, saved.Actresses, 1)
					}
				}
			})
		}
	}
}

func TestCreditSuppressionMultipartAtomicity(t *testing.T) {
	for _, failure := range []string{"none", "stale-sibling", "missing-sibling", "envelope"} {
		t.Run(failure, func(t *testing.T) {
			f := newSuppressionFixture(t, 2)
			old := f.read(t)
			f.suppress(t)
			req := contracts.UpdateMovieRequest{Movie: old.Results[f.paths[0]].Movie, ExpectedResultRevisions: map[string]uint64{"suppression-result-0": 1, "suppression-result-1": 1}}
			req.Movie.Code = ""
			req.Movie.Title = "Family after"
			want := http.StatusOK
			switch failure {
			case "stale-sibling":
				req.ExpectedResultRevisions["suppression-result-1"] = 0
				want = http.StatusConflict
			case "missing-sibling":
				delete(req.ExpectedResultRevisions, "suppression-result-1")
				want = http.StatusConflict
			case "envelope":
				require.NoError(t, f.deps.Repos.DB.Exec("CREATE TRIGGER fail_suppression_envelope BEFORE UPDATE ON jobs BEGIN SELECT RAISE(ABORT, 'synthetic envelope failure'); END").Error)
				want = http.StatusInternalServerError
			}
			response := f.patch(t, req, want)
			saved := f.stored(t)
			require.True(t, saved.Credits[0].Suppressed)
			require.Equal(t, "Saved override", saved.Credits[0].OverrideName)
			require.Empty(t, saved.Actresses)
			for _, p := range f.paths {
				result, err := f.job.Results().GetMovieResult(p)
				require.NoError(t, err)
				if want == http.StatusOK {
					require.Equal(t, uint64(2), result.Revision)
					require.Empty(t, result.Movie.Actresses)
					require.Equal(t, uint64(2), response.Revisions[result.ResultID])
				} else {
					require.Equal(t, uint64(1), result.Revision)
					require.Len(t, result.Movie.Actresses, 1)
				}
			}
			if want == http.StatusOK {
				require.Equal(t, "Family after", saved.Title)
				require.Equal(t, int64(2), saved.RenderGeneration)
			} else {
				require.Equal(t, "Before", saved.Title)
				require.Equal(t, int64(1), saved.RenderGeneration)
			}
		})
	}
}

// Changes made after the outer repo read must be observed inside WithEditTx.
type suppressionBeforeTx struct {
	db     *database.DB
	before func()
}

func (s suppressionBeforeTx) WithEditTx(ctx context.Context, fn func(database.EditUnit) error) error {
	s.before()
	return s.db.WithEditTx(ctx, fn)
}

func TestCreditSuppressionBetweenReadAndCommit(t *testing.T) {
	f := newSuppressionFixture(t, 1)
	// A new store allows injection at the actual composite transaction boundary.
	store := worker.NewJobStore(f.deps.Repos.JobRepo, f.deps.Repos.BatchFileOpRepo, f.deps.Repos.MovieRepo, "", nil, nil, worker.WithActressRepo(f.deps.Repos.ActressRepo), worker.WithEditTransactor(suppressionBeforeTx{db: f.deps.CoreDeps.DB, before: func() {
		f.suppress(t)
		require.NoError(t, f.deps.Repos.ActressRepo.RenameNameFields(t.Context(), f.actress.ID, "Concurrent canonical", "Name", ""))
	}}), worker.WithSkipStartupRecovery())
	job := store.CreateJobBatch([]string{"concurrent.mp4"})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	baseline := f.stored(t)
	setJobResult(job, "concurrent.mp4", &resultstore.MovieResult{ResultID: "concurrent-result", Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: "concurrent.mp4", MovieID: f.movie.ID}, Movie: baseline, PersistedMovie: true})
	f.deps.JobStore = store
	incoming := contracts.MovieViewFromModel(baseline)
	incoming.Title = "Concurrent save"
	rev := uint64(1)
	response := doJSON(t, f.router, http.MethodPatch, "/batch/"+job.GetID()+"/results/concurrent-result", contracts.UpdateMovieRequest{Movie: incoming, ExpectedResultRevision: &rev})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var echo contracts.MovieResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &echo))
	saved := f.stored(t)
	require.True(t, saved.Credits[0].Suppressed)
	require.Empty(t, saved.Actresses)
	require.Empty(t, echo.Movie.Actresses)
	require.Equal(t, "Concurrent canonical", saved.Credits[0].Actress.FirstName)
	require.Equal(t, "Concurrent save", saved.Title)
	require.GreaterOrEqual(t, saved.RenderGeneration, int64(2))
}

func TestCreditSuppressionLegacyCastVersion(t *testing.T) {
	for _, code := range []string{"wire", "omitted"} {
		for _, cast := range []string{"unchanged", "nil", "explicit"} {
			for _, withRevision := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/revision=%v", code, cast, withRevision), func(t *testing.T) {
					f := newSuppressionFixture(t, 1)
					old := f.read(t)
					f.suppress(t)
					view := old.Results[f.paths[0]].Movie
					view.CastVersion = ""
					view.Title = "Legacy save"
					if code == "omitted" {
						view.Code = ""
					}
					if cast == "nil" {
						view.Actresses = nil
					}
					if cast == "explicit" {
						view.Actresses[0].FirstName = "Changed"
					}
					req := contracts.UpdateMovieRequest{Movie: view}
					if withRevision {
						rev := uint64(1)
						req.ExpectedResultRevision = &rev
					}
					want := http.StatusOK
					if cast == "explicit" {
						want = http.StatusConflict
					}
					f.patch(t, req, want)
					saved := f.stored(t)
					require.True(t, saved.Credits[0].Suppressed)
					require.Empty(t, saved.Actresses)
					if want == http.StatusOK {
						require.Equal(t, "Legacy save", saved.Title)
					} else {
						require.Equal(t, "Before", saved.Title)
					}
				})
			}
		}
	}
}

func TestCreditSuppressionNoopAndDeletedMovie(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprintf("deleted=%v", deleted), func(t *testing.T) {
			f := newSuppressionFixture(t, 1)
			old := f.read(t)
			f.suppress(t)
			view := old.Results[f.paths[0]].Movie
			view.Code = ""
			if deleted {
				require.NoError(t, f.deps.Repos.DB.Delete(&f.movie).Error)
				f.patch(t, contracts.UpdateMovieRequest{Movie: view}, http.StatusConflict)
				_, err := f.deps.Repos.MovieRepo.FindByContentID(t.Context(), f.movie.ContentID)
				require.ErrorIs(t, err, database.ErrNotFound)
				return
			}
			f.patch(t, contracts.UpdateMovieRequest{Movie: view}, http.StatusOK)
			stored := f.stored(t)
			require.Equal(t, int64(1), stored.RenderGeneration, "unchanged metadata must not invalidate rendering")
			require.True(t, stored.Credits[0].Suppressed)
			require.True(t, stored.Credits[0].UserOverride)
			// The job baseline is now empty too: an explicit empty array echo remains
			// compatible without either result revision or cast_version.
			fresh := f.read(t).Results[f.paths[0]].Movie
			fresh.CastVersion = ""
			fresh.Actresses = []contracts.ActressView{}
			f.patch(t, contracts.UpdateMovieRequest{Movie: fresh}, http.StatusOK)
			require.Equal(t, int64(1), f.stored(t).RenderGeneration)
		})
	}
}

func TestCreditSuppressionPreserveLegacyCastWithoutCredits(t *testing.T) {
	f := newSuppressionFixture(t, 1)
	require.NoError(t, f.deps.Repos.DB.Delete(&f.credit).Error)
	view := contracts.MovieViewFromModel(f.stored(t))
	view.Title = "Legacy association"
	view.Code = ""
	view.CastVersion = ""
	f.patch(t, contracts.UpdateMovieRequest{Movie: view}, http.StatusOK)
	saved := f.stored(t)
	require.Len(t, saved.Actresses, 1)
	require.Equal(t, f.actress.ID, saved.Actresses[0].ID)
	require.Len(t, saved.Credits, 1)
	require.False(t, saved.Credits[0].Suppressed)
}

func TestCreditSuppressionFreshIdentityEchoNeedsNoRevision(t *testing.T) {
	f := newSuppressionFixture(t, 1)
	// Read-only identity provenance changes the cast version, not the user's
	// actress-edit intent. A current echo must remain compatible without CAS.
	require.NoError(t, f.deps.Repos.DB.Model(&models.Actress{}).Where("id = ?", f.actress.ID).Update("origin", "scrape").Error)
	view := f.read(t).Results[f.paths[0]].Movie
	require.NotEqual(t, models.ActressCastVersion([]models.Actress{f.actress}), view.CastVersion)
	view.Title = "Unchanged current identity"
	f.patch(t, contracts.UpdateMovieRequest{Movie: view}, http.StatusOK)
	saved := f.stored(t)
	require.Len(t, saved.Actresses, 1)
	require.Equal(t, "scrape", saved.Actresses[0].Origin)
	require.Equal(t, "Unchanged current identity", saved.Title)
}

// Candidate identities normally carry a NameKey. The wire DTO exposes it, but
// its inverse deliberately strips that server-owned value before persistence.
func newNamedSuppressionFixture(t *testing.T) *suppressionFixture {
	t.Helper()
	f := newSuppressionFixture(t, 1)
	require.NoError(t, f.deps.Repos.DB.Model(&models.Actress{}).Where("id = ?", f.actress.ID).Updates(map[string]any{"name_key": "name canonical", "verified": false, "origin": "scrape"}).Error)
	setJobResult(f.job, f.paths[0], &resultstore.MovieResult{ResultID: "suppression-result-0", Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: f.paths[0], MovieID: f.movie.ID}, Movie: f.stored(t), PersistedMovie: true})
	return f
}

func TestCreditSuppressionNamedCandidateEcho(t *testing.T) {
	for _, version := range []string{"old", "omitted"} {
		for _, revision := range []string{"current", "omitted", "stale"} {
			t.Run(version+"/"+revision, func(t *testing.T) {
				f := newNamedSuppressionFixture(t)
				wire := f.read(t)
				view := wire.Results[f.paths[0]].Movie
				require.Equal(t, "name canonical", view.Actresses[0].NameKey)
				require.False(t, view.Actresses[0].Verified)
				require.Equal(t, "scrape", view.Actresses[0].Origin)
				require.Empty(t, contracts.MovieViewToModel(view).Actresses[0].NameKey)
				f.suppress(t)
				view.Title = "Candidate metadata only"
				if version == "omitted" {
					view.CastVersion = ""
				}
				req := contracts.UpdateMovieRequest{Movie: view}
				want := http.StatusOK
				if revision != "omitted" {
					rev := wire.Results[f.paths[0]].Revision
					if revision == "stale" {
						rev--
						want = http.StatusConflict
					}
					req.ExpectedResultRevision = &rev
				}
				response := f.patch(t, req, want)
				saved := f.stored(t)
				require.True(t, saved.Credits[0].Suppressed)
				require.Empty(t, saved.Actresses)
				require.Equal(t, "name canonical", saved.Credits[0].Actress.NameKey)
				require.False(t, saved.Credits[0].Actress.Verified)
				require.Equal(t, "scrape", saved.Credits[0].Actress.Origin)
				if want == http.StatusOK {
					require.Equal(t, view.Title, saved.Title)
					require.Empty(t, response.Movie.Actresses)
				} else {
					require.Equal(t, "Before", saved.Title)
				}
			})
		}
	}
}

func TestCreditSuppressionNamedCandidateReadOnlyFields(t *testing.T) {
	f := newNamedSuppressionFixture(t)
	view := f.read(t).Results[f.paths[0]].Movie
	view.Actresses[0].NameKey = "forged key"
	view.Actresses[0].Verified = true
	view.Actresses[0].Origin = "forged"
	view.Credits[0].Suppressed = true
	view.Credits[0].OverrideName = "forged override"
	view.Credits[0].UserOverride = false
	view.Credits[0].OrderIndex = 100
	view.Title = "Accepted unrelated edit"
	f.patch(t, contracts.UpdateMovieRequest{Movie: view}, http.StatusOK)
	saved := f.stored(t)
	require.Equal(t, view.Title, saved.Title)
	require.Len(t, saved.Actresses, 1)
	require.Equal(t, "name canonical", saved.Actresses[0].NameKey)
	require.False(t, saved.Actresses[0].Verified)
	require.Equal(t, "scrape", saved.Actresses[0].Origin)
	require.False(t, saved.Credits[0].Suppressed)
	require.True(t, saved.Credits[0].UserOverride)
	require.Equal(t, "Saved override", saved.Credits[0].OverrideName)
	require.Equal(t, 3, saved.Credits[0].OrderIndex)
}

func TestCreditSuppressionNamedCandidateWritableIntent(t *testing.T) {
	baseline := &models.Movie{Actresses: []models.Actress{{ID: 7, FirstName: "Canonical", LastName: "Name", NameKey: "name canonical", Origin: "scrape"}}}
	for _, tc := range []struct {
		name string
		edit func(*contracts.ActressView)
	}{
		{"ID", func(a *contracts.ActressView) { a.ID++ }},
		{"DMMID", func(a *contracts.ActressView) { a.DMMID = 10 }},
		{"FirstName", func(a *contracts.ActressView) { a.FirstName = "Edited" }},
		{"LastName", func(a *contracts.ActressView) { a.LastName = "Edited" }},
		{"JapaneseName", func(a *contracts.ActressView) { a.JapaneseName = "Edited" }},
		{"ThumbURL", func(a *contracts.ActressView) { a.ThumbURL = "https://example.invalid/edited.jpg" }},
		{"Aliases", func(a *contracts.ActressView) { a.Aliases = "Edited" }},
		{"ThumbEdited", func(a *contracts.ActressView) { a.ThumbEdited = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := contracts.MovieViewFromModel(baseline)
			require.True(t, shouldPreserveCachedActresses(contracts.MovieViewToModel(view), baseline))
			tc.edit(&view.Actresses[0])
			require.False(t, shouldPreserveCachedActresses(contracts.MovieViewToModel(view), baseline))
		})
	}
}
