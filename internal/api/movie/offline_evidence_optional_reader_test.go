package movie

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestOfflineEvidenceMovieMissingOptionalReaderIsUnavailable(t *testing.T) {
	for _, rescrape := range []bool{false, true} {
		t.Run(map[bool]string{false: "scrape", true: "rescrape"}[rescrape], func(t *testing.T) {
			calls := 0
			wf := auditWorkflow{run: func(ctx context.Context) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
				calls++
				report := scrapeevidence.Resolve(ctx, nil, false)
				require.NotNil(t, report)
				require.Equal(t, scrapeevidence.Unavailable, report.Outcome)
				return &scrape.ScrapeResult{Status: scrape.StatusCompleted, Movie: &models.Movie{ID: "TEST-001", Title: "canonical"}, OfflineEvidence: report}, &workflow.OrchestrationMeta{}, nil
			}}
			router, request := evidenceMovieRouter(MovieDeps{WorkflowFn: func() workflow.WorkflowInterface { return wf }}, rescrape)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, 1, calls)
			var body struct {
				Movie           *contracts.MovieView   `json:"movie"`
				OfflineEvidence *scrapeevidence.Report `json:"offline_evidence"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.NotNil(t, body.Movie)
			require.Equal(t, "canonical", body.Movie.Title)
			require.NotNil(t, body.OfflineEvidence)
			require.Equal(t, scrapeevidence.Unavailable, body.OfflineEvidence.Outcome)
		})
	}
}

type evidenceAuditDeadlineRepo struct {
	database.HistoryRepositoryInterface
	create func(context.Context, *models.History) error
}

func (r evidenceAuditDeadlineRepo) Create(ctx context.Context, history *models.History) error {
	return r.create(ctx, history)
}

func TestOfflineEvidenceMovieTimeoutAuditKeepsOperationDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deadline := time.Now().Add(time.Second)
		ctx, cancel := context.WithDeadline(t.Context(), deadline)
		defer cancel()
		calls := 0
		history := evidenceAuditDeadlineRepo{create: func(ctx context.Context, record *models.History) error {
			calls++
			got, ok := ctx.Deadline()
			require.True(t, ok)
			require.Equal(t, deadline, got, "audit must not get a fresh detached five seconds")
			require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
			require.Equal(t, models.HistoryStatusFailed, record.Status)
			require.Equal(t, "scrape timed out", record.ErrorMessage)
			return ctx.Err()
		}}
		wf := auditWorkflow{run: func(ctx context.Context) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		}}
		router, request := evidenceMovieRouter(MovieDeps{HistoryRepo: history, WorkflowFn: func() workflow.WorkflowInterface { return wf }, RequestTimeoutFn: func() time.Duration { return time.Hour }}, false)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request.WithContext(ctx))
		require.Equal(t, http.StatusGatewayTimeout, response.Code)
		require.Equal(t, 1, calls)
		require.NotContains(t, response.Body.String(), "offline_evidence")
	})
}
