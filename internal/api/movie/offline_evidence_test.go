package movie

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func evidenceMovieRouter(deps MovieDeps, rescrape bool) (*gin.Engine, *http.Request) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	path, body := "/scrape", `{"id":"TEST-001","show_offline_evidence":true}`
	if rescrape {
		r.POST("/movies/:id/rescrape", rescrapeMovie(deps))
		path = "/movies/TEST-001/rescrape"
		body = `{"selected_scrapers":["r18dev"],"show_offline_evidence":true}`
	} else {
		r.POST("/scrape", scrapeMovie(deps))
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return r, req
}

type evidenceCancelPoster struct{ entered chan context.Context }

func (p evidenceCancelPoster) GeneratePoster(ctx context.Context, _ string, _ *models.Movie) error {
	p.entered <- ctx
	<-ctx.Done()
	return ctx.Err()
}

func TestOfflineEvidenceMovieDeadlineCoversAdmission(t *testing.T) {
	for _, rescrape := range []bool{false, true} {
		t.Run(map[bool]string{false: "scrape", true: "rescrape"}[rescrape], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				released, called := false, false
				deps := MovieDeps{RequestTimeoutFn: func() time.Duration { return time.Second }, WorkflowFn: func() workflow.WorkflowInterface { called = true; return nil }, BeginOfflineEvidence: func(ctx context.Context, _ bool) (context.Context, func(), error) {
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					require.Equal(t, time.Second, time.Until(deadline))
					<-ctx.Done()
					return ctx, func() { released = true }, nil
				}}
				r, req := evidenceMovieRouter(deps, rescrape)
				response := httptest.NewRecorder()
				r.ServeHTTP(response, req)
				require.Equal(t, http.StatusGatewayTimeout, response.Code)
				require.True(t, released)
				require.False(t, called)
			})
		})
	}
}

func TestOfflineEvidenceMovieShutdownReachesPosterAndJoins(t *testing.T) {
	for _, rescrape := range []bool{false, true} {
		t.Run(map[bool]string{false: "scrape", true: "rescrape"}[rescrape], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rt := core.NewAPIRuntime(nil)
				defer rt.Shutdown()
				poster := evidenceCancelPoster{entered: make(chan context.Context, 1)}
				wf := auditWorkflow{run: func(ctx context.Context) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
					return &scrape.ScrapeResult{Status: scrape.StatusCompleted, Movie: &models.Movie{ID: "TEST-001"}, OfflineEvidence: scrapeevidence.Resolve(ctx, nil, false)}, &workflow.OrchestrationMeta{}, nil
				}}
				deps := MovieDeps{WorkflowFn: func() workflow.WorkflowInterface { return wf }, PosterGen: poster, BeginOfflineEvidence: rt.BeginOfflineScrapeEvidence}
				r, req := evidenceMovieRouter(deps, rescrape)
				response := httptest.NewRecorder()
				served := make(chan struct{})
				go func() { r.ServeHTTP(response, req); close(served) }()
				synctest.Wait()
				ctx := <-poster.entered
				require.NoError(t, ctx.Err())
				rt.Shutdown()
				synctest.Wait()
				<-served
				require.ErrorIs(t, ctx.Err(), context.Canceled)
				require.Equal(t, http.StatusGatewayTimeout, response.Code)
				require.NotContains(t, response.Body.String(), "offline_evidence")
				// Closed admission must precede workflow access even after HTTP drain.
				deps.WorkflowFn = func() workflow.WorkflowInterface { t.Fatal("workflow touched after shutdown"); return nil }
				r, req = evidenceMovieRouter(deps, rescrape)
				response = httptest.NewRecorder()
				r.ServeHTTP(response, req)
				require.Equal(t, http.StatusServiceUnavailable, response.Code)
			})
		})
	}
}

func TestOfflineEvidenceMovieEarlyDeadlineFailureAndPanicRelease(t *testing.T) {
	earlier := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), earlier)
	defer cancel()
	deps := MovieDeps{RequestTimeoutFn: func() time.Duration { return time.Hour }, BeginOfflineEvidence: func(ctx context.Context, _ bool) (context.Context, func(), error) {
		got, ok := ctx.Deadline()
		require.True(t, ok)
		require.Equal(t, earlier, got)
		return ctx, nil, errors.New("admission refused")
	}}
	_, release, err := deps.beginEvidence(ctx, true)
	require.Error(t, err)
	require.Nil(t, release)
	rt := core.NewAPIRuntime(nil)
	defer rt.Shutdown()
	deps = MovieDeps{BeginOfflineEvidence: rt.BeginOfflineScrapeEvidence, WorkflowFn: func() workflow.WorkflowInterface { panic("test workflow panic") }}
	r, req := evidenceMovieRouter(deps, false)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.True(t, rt.WaitBackgroundTasks(time.Second))
	rt.Shutdown()
	// Default-off does not invoke the read/admission seam, including inherited sessions.
	pinned, done := (*scrapeevidence.Service)(nil).Begin(t.Context(), true)
	defer done()
	deps.BeginOfflineEvidence = func(context.Context, bool) (context.Context, func(), error) {
		t.Fatal("default-off reader called")
		return nil, nil, nil
	}
	off, done, err := deps.beginEvidence(pinned, false)
	require.NoError(t, err)
	defer done()
	require.Nil(t, scrapeevidence.Resolve(off, nil, false))
}
