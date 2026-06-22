package batch

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
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
