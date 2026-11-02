package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type defensiveHistoryRecorder struct {
	database.HistoryRepositoryInterface
	rows []models.History
}

func (r *defensiveHistoryRecorder) Create(_ context.Context, h *models.History) error {
	r.rows = append(r.rows, *h)
	return nil
}

func TestAuditOrganizeFailureSuppressesWrappedCancellation(t *testing.T) {
	repo := &defensiveHistoryRecorder{}
	inputs := applyPhaseInputs{HistoryRepo: repo, JobID: "defensive-audit", OperationMode: "organize"}
	movie := &models.Movie{ID: "TEST-001"}
	// The normal caller already excludes cancellation. This directly specifies
	// the helper's own defensive boundary, not another runtime cancellation path.
	auditOrganizeFailure(inputs, movie, "file.mp4", nil, fmt.Errorf("interrupted: %w", context.Canceled), ApplyPhaseConfig{})
	require.Empty(t, repo.rows)
	// Positive control: the recorder is live and deadline failures still audit.
	auditOrganizeFailure(inputs, movie, "file.mp4", nil, context.DeadlineExceeded, ApplyPhaseConfig{})
	require.Len(t, repo.rows, 1)
	require.Equal(t, models.HistoryStatusFailed, repo.rows[0].Status)
	require.Equal(t, models.HistoryOpOrganize, repo.rows[0].Operation)
	require.Equal(t, context.DeadlineExceeded.Error(), repo.rows[0].ErrorMessage)
}

func TestAuditRescrapeFailureAllowsAbsentOptionalRepository(t *testing.T) {
	require.NotPanics(t, func() {
		auditRescrapeFailure(rescrapePhaseInputs{}, "TEST-001", "file.mp4", fmt.Errorf("scrape failed"))
	})
	// Contrast the optional-repository no-op with an available audit sink.
	repo := &defensiveHistoryRecorder{}
	auditRescrapeFailure(rescrapePhaseInputs{HistoryRepo: repo}, "TEST-001", "file.mp4", fmt.Errorf("scrape failed"))
	require.Len(t, repo.rows, 1)
	require.Equal(t, models.HistoryOpScrape, repo.rows[0].Operation)
	require.Equal(t, models.HistoryStatusFailed, repo.rows[0].Status)
}
