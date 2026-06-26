package movie

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/workflow"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// compareNFO godoc
// @Summary Compare NFO with scraped data
// @Description Compare existing NFO file with freshly scraped metadata, showing differences and merge preview
// @Tags movies
// @Accept json
// @Produce json
// @Param id path string true "Movie ID" example:"IPX-535"
// @Param request body contracts.NFOComparisonRequest true "Comparison options"
// @Success 200 {object} contracts.NFOComparisonResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/movies/{id}/compare-nfo [post]
func compareNFO(deps MovieDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		movieID := c.Param("id")

		var req contracts.NFOComparisonRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// Only a truly empty body (io.EOF) may fall back to defaults. Any other
			// bind error (malformed JSON, type mismatch) is a real client error and
			// must not be silently turned into the later "nfo_path is required"
			// response, which would hide the actual cause from the caller.
			if !errors.Is(err, io.EOF) {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "invalid request body: " + err.Error()})
				return
			}
			req = contracts.NFOComparisonRequest{}
		}

		// Step 1: Validate and sanitize NFO path (HTTP-layer security concern)
		if req.NFOPath == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "nfo_path is required for comparison"})
			return
		}

		// Get allowed directories from service for path validation
		allowedDirs := deps.getAllowedDirs()

		// Validate the NFO path against security constraints
		validatedPath, err := validateNFOPath(req.NFOPath, allowedDirs)
		if err != nil {
			if errors.Is(err, ErrNFONotFound) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: err.Error()})
			} else if errors.Is(err, ErrNFOAccessDenied) {
				c.JSON(http.StatusForbidden, contracts.ErrorResponse{Error: err.Error()})
			} else {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			}
			return
		}

		// Step 2: Call Compare seam — handles scrape-aggregate-merge pipeline internally
		wf := deps.getWorkflow()
		if wf == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "workflow not available"})
			return
		}

		// Per ADR-0030: resolve strategy seam strings at the boundary through
		// the shared workflow.ResolveSeamStrings — same path as batch and TUI handlers.
		resolved, resolveErr := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
			Preset:         req.Preset,
			ScalarStrategy: req.ScalarStrategy,
			ArrayStrategy:  req.ArrayStrategy,
		})
		if resolveErr != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: resolveErr.Error()})
			return
		}

		result, err := wf.Compare(c.Request.Context(), workflow.CompareCmd{
			MovieID:          movieID,
			NFOPath:          validatedPath,
			ScalarStrategy:   resolved.ScalarStrategy,
			ArrayStrategy:    resolved.ArrayStrategy,
			SelectedScrapers: req.SelectedScrapers,
		})
		if err != nil {
			// Map typed seam errors to appropriate HTTP status codes.
			// Per GL2-3: use errors.Is() instead of strings.Contains()
			// so the API layer is decoupled from error message text.
			switch {
			case errors.Is(err, workflow.ErrInvalidPreset):
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			case errors.Is(err, workflow.ErrNFOParseFailed):
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to parse NFO file"})
			case errors.Is(err, workflow.ErrScrapeFailed), errors.Is(err, workflow.ErrScrapeNoResult):
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "No scraped data available for comparison"})
			case errors.Is(err, workflow.ErrMergeFailed):
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			}
			return
		}

		// Step 3: Map CompareResult to API response
		response := contracts.NFOComparisonResponse{
			MovieID:     movieID,
			NFOExists:   result.NFOExists,
			NFOPath:     result.NFOPath,
			NFOData:     contracts.MovieViewFromModel(result.NFOData),
			ScrapedData: contracts.MovieViewFromModel(result.ScrapedData),
			MergedData:  contracts.MovieViewFromModel(result.Movie),
		}

		// Derive provenance from Differences — the merged value tells us which source won.
		// Per ADR-0037: MergeProvenance zombie removed; field-level source is derived from
		// Differences (NFOValue/ScrapedValue vs MergedValue).
		if len(result.Differences) > 0 {
			apiProvenance := make(map[string]contracts.DataSource, len(result.Differences))
			for _, d := range result.Differences {
				source := deriveProvenanceSource(d)
				apiProvenance[strings.ToLower(d.Field)] = source
			}
			response.Provenance = apiProvenance
		}

		// Convert merge stats to API format
		if result.MergeStats != nil {
			response.MergeStats = &contracts.MergeStatistics{
				TotalFields:       result.MergeStats.TotalFields,
				FromScraper:       result.MergeStats.FromScraper,
				FromNFO:           result.MergeStats.FromNFO,
				MergedArrays:      result.MergeStats.MergedArrays,
				ConflictsResolved: result.MergeStats.ConflictsResolved,
				EmptyFields:       result.MergeStats.EmptyFields,
			}
		}

		// Step 4: Map differences from the Compare seam result.
		// Domain logic (identifyDifferences) lives behind the seam;
		// the API layer maps contracts.FieldDifference → contracts.FieldDifference.
		if len(result.Differences) > 0 {
			response.Differences = make([]contracts.FieldDifference, len(result.Differences))
			for i, d := range result.Differences {
				response.Differences[i] = contracts.FieldDifference{
					Field:        d.Field,
					NFOValue:     d.NFOValue,
					ScrapedValue: d.ScrapedValue,
					MergedValue:  d.MergedValue,
				}
			}
		}

		c.JSON(http.StatusOK, response)
	}
}

// deriveProvenanceSource determines which source "won" for a given field difference.
// It compares the MergedValue against NFOValue and ScrapedValue to classify the source.
func deriveProvenanceSource(d workflow.FieldDifference) contracts.DataSource {
	source := "merged"
	switch {
	case reflect.DeepEqual(d.MergedValue, d.NFOValue):
		source = "nfo"
	case reflect.DeepEqual(d.MergedValue, d.ScrapedValue):
		source = "scraper"
	}
	return contracts.DataSource{
		Source: source,
	}
}
