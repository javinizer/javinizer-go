package batch

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// TestUpdateBatchMovie_FanOutUsesCanonicalMovieIdentity pins Codex round-9
// P0: the whole-movie PATCH fan-out (the file paths receiving the edit) must
// resolve through the SAME canonical identity the poster lock and cache key
// on — posterLockKeyFor (Movie.ID when set, FileMatchInfo.MovieID otherwise).
// The fixture is the divergent state FMI=OLDK / Movie.ID=NEWK (a result
// re-keyed by its stored movie before its FileMatchInfo converged) PLUS a
// bystander movie that legitimately still lives at OLDK: pre-fix
// lookupResultByResultID fanned out via FileMatchInfo.MovieID, so the PATCH's
// multipart UpdateMovie loop REWROTE THE BYSTANDER movie while never touching
// the PATCHed result's own NEWK family. Parity with the crop/from-URL
// identity fix pinned in movie_edit_poster_identity_test.go.
func TestUpdateBatchMovie_FanOutUsesCanonicalMovieIdentity(t *testing.T) {
	srv := newPosterConcurrencyServer(t)
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const oldKey, newKey = "PANF-A", "PANF-B"
	fileOther := "/path/to/a-fanout-bystander.mp4" // another movie AT the stale key
	fileTarget := "/path/to/z-fanout-target.mp4"   // the divergent PATCH target
	job := createJobWithWF(deps, cfg, []string{fileOther, fileTarget})
	setJobResult(job, fileOther, &resultstore.MovieResult{
		ResultID:      "res-fanout-bystander",
		FileMatchInfo: models.FileMatchInfo{Path: fileOther, MovieID: oldKey},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldKey, Title: "Bystander", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})
	setJobResult(job, fileTarget, &resultstore.MovieResult{
		ResultID:      "res-fanout-diverged",
		FileMatchInfo: models.FileMatchInfo{Path: fileTarget, MovieID: oldKey}, // not yet converged
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: newKey, Title: "Effective", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	// Title-only edit carrying the effective ID and the UNCHANGED poster
	// source (no rename, no cache refresh — the fan-out is the only mutation).
	code := patchWholeMovie(t, router, job.GetID(), "res-fanout-diverged", newKey, srv.URL+"/old.jpg")
	require.Equal(t, http.StatusOK, code)

	// The PATCHed result's own family received the edit…
	target := storedMovieResultByPath(t, job, fileTarget)
	require.NotNil(t, target.Movie)
	assert.Equal(t, newKey, target.Movie.ID)
	assert.Equal(t, "Race", target.Movie.Title)

	// …and the bystander movie at the STALE key was NOT rewritten: pre-fix
	// the fan-out keyed on FileMatchInfo.MovieID(=OLDK) and stamped the
	// request movie ("Race"/NEWK) onto the bystander.
	other := storedMovieResultByPath(t, job, fileOther)
	require.NotNil(t, other.Movie)
	assert.Equal(t, oldKey, other.Movie.ID, "the bystander's identity must be untouched")
	assert.Equal(t, "Bystander", other.Movie.Title,
		"the PATCH must not fan out onto the stale FileMatchInfo.MovieID family")

	// No poster refresh fired: the source was unchanged.
	assert.Equal(t, int64(0), srv.hits["/old.jpg"].Load())
	assert.Equal(t, int64(0), srv.hits["/a.jpg"].Load())
	assert.Equal(t, int64(0), srv.hits["/b.jpg"].Load())

	assertPosterSourceLockFreeAPI(t, job.GetID(), oldKey)
	assertPosterSourceLockFreeAPI(t, job.GetID(), newKey)
}
