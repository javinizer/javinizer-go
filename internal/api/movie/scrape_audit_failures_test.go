package movie

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

type auditWorkflow struct {
	workflow.WorkflowInterface
	run func(context.Context) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error)
}

func (w auditWorkflow) Scrape(ctx context.Context, _ scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return w.run(ctx)
}

type auditPoster struct {
	calls int
	job   string
	movie *models.Movie
}

func (p *auditPoster) GeneratePoster(_ context.Context, job string, movie *models.Movie) error {
	p.calls++
	p.job = job
	p.movie = movie
	return errors.New("poster unavailable")
}

func TestScrapeMovieFailureAuditRedaction(t *testing.T) {
	const input = "https://alice:secret@example.com/movie?token=private#session"
	const safe = "https://redacted:redacted@example.com/movie"
	for _, mode := range []string{"timeout", "workflow error", "failed scraper", "cancelled error"} {
		t.Run(mode, func(t *testing.T) {
			d := createTestDeps(t, &config.Config{}, "")
			wf := auditWorkflow{run: func(ctx context.Context) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
				switch mode {
				case "timeout":
					<-ctx.Done()
					return nil, nil, ctx.Err()
				case "workflow error":
					return nil, nil, errors.New("fetch failed: " + input)
				case "cancelled error":
					return nil, nil, context.Canceled
				default:
					return &scrape.ScrapeResult{Status: scrape.StatusFailed, Message: "fetch failed: " + input, ScraperResults: []*models.ScraperResult{{}, {ID: "AUDIT-001"}}}, nil, nil
				}
			}}
			deps := MovieDeps{HistoryRepo: d.Repos.HistoryRepo, WorkflowFn: func() workflow.WorkflowInterface { return wf }}
			if mode == "timeout" {
				deps.RequestTimeoutFn = func() time.Duration { return time.Millisecond }
			}
			router := gin.New()
			router.POST("/scrape", scrapeMovie(deps))
			req := httptest.NewRequest(http.MethodPost, "/scrape", strings.NewReader(`{"id":"`+input+`"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			wantStatus := http.StatusInternalServerError
			if mode == "timeout" {
				wantStatus = http.StatusGatewayTimeout
			}
			if mode == "failed scraper" {
				wantStatus = http.StatusNotFound
			}
			require.Equal(t, wantStatus, rec.Code)
			rows, err := d.Repos.HistoryRepo.FindByOperation(context.Background(), models.HistoryOpScrape, 10)
			require.NoError(t, err)
			if mode == "cancelled error" {
				require.Empty(t, rows)
				return
			}
			require.Len(t, rows, 1)
			row := rows[0]
			require.Equal(t, safe, row.OriginalPath)
			require.Equal(t, models.HistoryStatusFailed, row.Status)
			wantID := safe
			if mode == "failed scraper" {
				wantID = "AUDIT-001"
			}
			require.Equal(t, wantID, row.MovieID)
			wantError := "fetch failed: " + safe
			if mode == "timeout" {
				wantError = "scrape timed out"
			}
			require.Equal(t, wantError, row.ErrorMessage)
		})
	}
}
func TestScrapeMoviePosterFailureIsNonFatal(t *testing.T) {
	movie := &models.Movie{ID: "POSTER-001"}
	wf := auditWorkflow{run: func(context.Context) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
		return &scrape.ScrapeResult{Status: scrape.StatusCompleted, Movie: movie}, &workflow.OrchestrationMeta{}, nil
	}}
	poster := &auditPoster{}
	router := gin.New()
	router.POST("/scrape", scrapeMovie(MovieDeps{WorkflowFn: func() workflow.WorkflowInterface { return wf }, PosterGen: poster}))
	req := httptest.NewRequest(http.MethodPost, "/scrape", strings.NewReader(`{"id":"POSTER-001"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, poster.calls)
	require.Equal(t, models.SentinelJobID().String(), poster.job)
	require.Same(t, movie, poster.movie)
}
