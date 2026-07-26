package worker

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

func recordHistory(ctx context.Context, repo database.HistoryRepositoryInterface, h models.History) {
	if repo == nil {
		return
	}
	h.ErrorMessage = redactEmbeddedURLs(h.ErrorMessage)
	if err := repo.Create(ctx, &h); err != nil {
		logging.Warnf("[history] create failed for %s %s: %v", h.Operation, h.MovieID, err)
	}
}

func jobIDPtr(id models.JobID) *string {
	s := string(id)
	if s == "" {
		return nil
	}
	return &s
}

func nilGuardOrganizeNewPath(result *workflow.ApplyResult) string {
	if result == nil || result.OrganizeResult == nil {
		return ""
	}
	return result.OrganizeResult.NewPath
}

func organizeMetadata(mode string, result *workflow.ApplyResult) string {
	steps := "{}"
	if result != nil {
		if b, err := json.Marshal(result.Steps); err == nil {
			steps = string(b)
		}
	}
	m := map[string]any{
		"operation_mode": mode,
		"steps":          json.RawMessage(steps),
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func historyAuditContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// auditScrapeFailure writes a failed scrape history row for a non-cancellation error or panic.
// It is called from trackScrapeResults for outcomes that were NOT handled by persistScrapeOutcome.
func auditScrapeFailure(inputs scrapePhaseInputs, o scrapeFileOutcome) {
	if inputs.HistoryRepo == nil || o.Cancelled {
		return
	}
	auditCtx, cancel := historyAuditContext()
	defer cancel()
	errMsg := o.ErrorMsg
	if o.Panic {
		errMsg = o.PanicMsg
	}
	if errMsg == "" {
		return
	}
	recordHistory(auditCtx, inputs.HistoryRepo, models.History{
		MovieID:      o.MovieID,
		BatchJobID:   jobIDPtr(inputs.JobID),
		Operation:    models.HistoryOpScrape,
		OriginalPath: o.FilePath,
		Status:       models.HistoryStatusFailed,
		ErrorMessage: errMsg,
	})
}

// auditScrapeSuccess writes a success scrape history row for outcomes NOT already handled by persist.
// Called from trackScrapeResults when recordedSuccesses does not contain the file path.
func auditScrapeSuccess(inputs scrapePhaseInputs, o scrapeFileOutcome) {
	if inputs.HistoryRepo == nil || o.Cancelled || !o.Success {
		return
	}
	if o.Result == nil || o.Result.Movie == nil {
		return
	}
	auditCtx, cancel := historyAuditContext()
	defer cancel()
	recordHistory(auditCtx, inputs.HistoryRepo, models.History{
		MovieID:      o.Result.Movie.ID,
		BatchJobID:   jobIDPtr(inputs.JobID),
		Operation:    models.HistoryOpScrape,
		OriginalPath: o.FilePath,
		Status:       models.HistoryStatusSuccess,
	})
}

// auditOrganizeSuccess writes a success organize history row.
// Only called when organize actually ran (result.OrganizeResult != nil) or when organizing
// was cancelled after a partial move (OrganizeResult exists despite context.Canceled).
func auditOrganizeSuccess(inputs applyPhaseInputs, movie *models.Movie, filePath string, result *workflow.ApplyResult, cfg ApplyPhaseConfig) {
	if inputs.HistoryRepo == nil || result == nil || result.OrganizeResult == nil {
		return
	}
	auditCtx, cancel := historyAuditContext()
	defer cancel()
	recordHistory(auditCtx, inputs.HistoryRepo, models.History{
		MovieID:      movie.ID,
		BatchJobID:   jobIDPtr(inputs.JobID),
		Operation:    models.HistoryOpOrganize,
		OriginalPath: filePath,
		NewPath:      result.OrganizeResult.NewPath,
		Status:       models.HistoryStatusSuccess,
		DryRun:       cfg.DryRun,
		Metadata:     organizeMetadata(inputs.OperationMode, result),
	})
}

// auditOrganizeFailure writes a failed organize history row for non-cancellation failures.
// Called from interpretApplyResult when applyErr is non-nil and not context.Canceled.
// Writes regardless of whether OrganizeResult exists — organize was requested and failed.
func auditOrganizeFailure(inputs applyPhaseInputs, movie *models.Movie, filePath string, result *workflow.ApplyResult, applyErr error, cfg ApplyPhaseConfig) {
	if inputs.HistoryRepo == nil {
		return
	}
	if applyErr != nil && errors.Is(applyErr, context.Canceled) {
		return
	}
	auditCtx, cancel := historyAuditContext()
	defer cancel()
	errMsg := ""
	if applyErr != nil {
		errMsg = applyErr.Error()
	}
	// Redact any URLs that may contain credentials in the error message
	errMsg = redactEmbeddedURLs(errMsg)
	recordHistory(auditCtx, inputs.HistoryRepo, models.History{
		MovieID:      movie.ID,
		BatchJobID:   jobIDPtr(inputs.JobID),
		Operation:    models.HistoryOpOrganize,
		OriginalPath: filePath,
		NewPath:      nilGuardOrganizeNewPath(result),
		Status:       models.HistoryStatusFailed,
		ErrorMessage: errMsg,
		DryRun:       cfg.DryRun,
		Metadata:     organizeMetadata(inputs.OperationMode, result),
	})
}

// auditOrganizePanic writes a failed organize history row for panics recovered outside interpretApplyResult.
// Called from trackApplyResults for o.Panic outcomes only (not o.Failed, to avoid double-write).
func auditOrganizePanic(inputs applyPhaseInputs, o applyFileOutcome) {
	if inputs.HistoryRepo == nil || o.Cancelled || !o.Panic {
		return
	}
	auditCtx, cancel := historyAuditContext()
	defer cancel()
	recordHistory(auditCtx, inputs.HistoryRepo, models.History{
		MovieID:      o.MovieID,
		BatchJobID:   jobIDPtr(inputs.JobID),
		Operation:    models.HistoryOpOrganize,
		OriginalPath: o.FilePath,
		Status:       models.HistoryStatusFailed,
		ErrorMessage: o.PanicMsg,
		DryRun:       o.DryRun,
	})
}

// auditRescrapeSuccess writes a success scrape history row for a completed rescrape.
var workerEmbeddedURLRe = regexp.MustCompile(`(?i)https?://[^\s/]+@`)
var workerQueryURLRe = regexp.MustCompile(`(?i)(https?://[^\s?#]+)[?#][^\s]+`)

func redactEmbeddedURLs(s string) string {
	s = workerEmbeddedURLRe.ReplaceAllString(s, "https://redacted:redacted@")
	s = workerQueryURLRe.ReplaceAllString(s, "$1")
	return s
}

func auditRescrapeSuccess(inputs rescrapePhaseInputs, movieID, filePath string) {
	if inputs.HistoryRepo == nil {
		return
	}
	auditCtx, cancel := historyAuditContext()
	defer cancel()
	recordHistory(auditCtx, inputs.HistoryRepo, models.History{
		MovieID:      movieID,
		BatchJobID:   jobIDPtr(inputs.JobID),
		Operation:    models.HistoryOpScrape,
		OriginalPath: filePath,
		Status:       models.HistoryStatusSuccess,
		Metadata:     organizeMetadata("rescrape", nil),
	})
}

// auditRescrapeFailure writes a failed scrape history row for a non-cancellation rescrape failure.
func auditRescrapeFailure(inputs rescrapePhaseInputs, movieID, filePath string, err error) {
	if inputs.HistoryRepo == nil {
		return
	}
	if err != nil && errors.Is(err, context.Canceled) {
		return
	}
	auditCtx, cancel := historyAuditContext()
	defer cancel()
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	recordHistory(auditCtx, inputs.HistoryRepo, models.History{
		MovieID:      movieID,
		BatchJobID:   jobIDPtr(inputs.JobID),
		Operation:    models.HistoryOpScrape,
		OriginalPath: filePath,
		Status:       models.HistoryStatusFailed,
		ErrorMessage: errMsg,
		Metadata:     organizeMetadata("rescrape", nil),
	})
}
