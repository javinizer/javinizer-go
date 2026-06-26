package batch

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

func validateRescrapeRequest(req *contracts.BatchRescrapeRequest) (int, string) {
	if req.ManualSearchInput != "" {
		cleaned := strings.Map(func(r rune) rune {
			if r == '\u200B' || r == '\u200C' || r == '\u200D' || r == '\uFEFF' {
				return -1
			}
			return r
		}, req.ManualSearchInput)

		cleaned = strings.TrimSpace(cleaned)

		if cleaned == "" {
			return http.StatusBadRequest, "Manual search input cannot be empty"
		}

		// Apply the cleaned value back to the request so downstream consumers
		// use the sanitized version. This mutation is intentional and
		// documented — the cleaned value replaces the raw input.
		req.ManualSearchInput = cleaned
	}

	if _, err := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
		Preset:         req.Preset,
		ScalarStrategy: req.ScalarStrategy,
		ArrayStrategy:  req.ArrayStrategy,
	}); err != nil {
		return http.StatusBadRequest, err.Error()
	}

	if len(req.SelectedScrapers) == 0 && req.ManualSearchInput == "" {
		return http.StatusBadRequest, "either selected_scrapers or manual_search_input must be provided"
	}

	return 0, ""
}

// resolveRescrapeMergeOptions resolves the merge-strategy seam strings from a
// BatchRescrapeRequest into a workflow.MergeOptions (per ADR-0030: preset is
// resolved at this boundary and overrides scalar/array). Returns the resolved
// MergeOptions and a bool indicating whether the caller actually supplied any
// merge options (preset/scalar/array non-empty), so RescrapeCmd.MergeEnabled
// can be set accurately. When the caller supplied nothing, the returned
// MergeOptions is zero and the bool is false — preserving the historical
// wholesale-replace rescrape behavior.
//
// Assumes the request was already validated by validateRescrapeRequest (which
// rejects invalid preset/scalar/array values); a defensive error return is
// kept for direct callers that skip validation.
func resolveRescrapeMergeOptions(req *contracts.BatchRescrapeRequest) (workflow.MergeOptions, bool, error) {
	supplied := req.Preset != "" || req.ScalarStrategy != "" || req.ArrayStrategy != ""
	if !supplied {
		return workflow.MergeOptions{}, false, nil
	}
	resolved, err := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
		Preset:         req.Preset,
		ScalarStrategy: req.ScalarStrategy,
		ArrayStrategy:  req.ArrayStrategy,
	})
	if err != nil {
		return workflow.MergeOptions{}, false, err
	}
	return workflow.MergeOptions{
		ScalarStrategy: resolved.ScalarStrategy,
		ArrayStrategy:  resolved.ArrayStrategy,
	}, true, nil
}

func writeErrorResponse(c *gin.Context, status int, isGone bool, errMsg string) {
	if isGone {
		c.JSON(status, gin.H{
			"error":   errMsg,
			"skipped": true,
		})
		return
	}
	c.JSON(status, contracts.ErrorResponse{Error: errMsg})
}

// rescrapeNotAllowed reports whether a job's current status prevents rescrape.
// Rescrape is only allowed when the job is Pending (not yet run) or Completed
// (finished, safe to re-run). All other states — Running, Cancelled, Failed,
// etc. — are rejected. The former redundant `snap.Status == JobStatusRunning`
// clause is subsumed since Running is neither Pending nor Completed.
func rescrapeNotAllowed(snap *worker.BatchJobStatus) bool {
	// Logically deleted jobs are never rescrapeable, even if their status is
	// still Pending or Completed — rescrape must return the 410 deleted path.
	return snap.IsDeleted || (snap.Status != models.JobStatusPending && snap.Status != models.JobStatusCompleted)
}
