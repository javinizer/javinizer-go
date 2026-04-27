package movie

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
)

// compareNFO godoc
// @Summary Compare NFO with scraped data
// @Description Compare existing NFO file with freshly scraped metadata, showing differences and merge preview
// @Tags movies
// @Accept json
// @Produce json
// @Param id path string true "Movie ID" example:"IPX-535"
// @Param request body NFOComparisonRequest false "Comparison options"
// @Success 200 {object} NFOComparisonResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/movies/{id}/compare-nfo [post]
func compareNFO(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		movieID := c.Param("id")

		var req NFOComparisonRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// Allow empty body - use defaults
			req = NFOComparisonRequest{}
		}

		response := NFOComparisonResponse{
			MovieID: movieID,
		}

		// Step 1: Validate and sanitize NFO path
		if req.NFOPath == "" {
			c.JSON(400, ErrorResponse{Error: "nfo_path is required for comparison"})
			return
		}

		// Get allowed directories from config for path validation
		allowedDirs := deps.GetConfig().API.Security.AllowedDirectories

		// Validate the NFO path against security constraints
		validatedPath, err := validateNFOPath(req.NFOPath, allowedDirs)
		if err != nil {
			// Return appropriate HTTP status based on error type using sentinel errors
			if errors.Is(err, ErrNFONotFound) {
				response.NFOExists = false
				c.JSON(404, ErrorResponse{Error: err.Error()})
			} else if errors.Is(err, ErrNFOAccessDenied) {
				c.JSON(403, ErrorResponse{Error: err.Error()})
			} else {
				c.JSON(400, ErrorResponse{Error: err.Error()})
			}
			return
		}

		response.NFOExists = true
		// Only return the filename (not absolute path) to avoid disclosing server directory structure
		response.NFOPath = filepath.Base(validatedPath)

		// Step 2: Parse NFO file
		parseResult, err := nfo.ParseNFO(afero.NewOsFs(), validatedPath)
		if err != nil {
			c.JSON(500, ErrorResponse{Error: "Failed to parse NFO file"})
			return
		}
		response.NFOData = parseResult.Movie

		// Step 3: Scrape fresh data
		parsed, err := matcher.ParseInput(movieID, deps.GetRegistry())
		if err != nil {
			c.JSON(400, ErrorResponse{Error: fmt.Sprintf("Invalid movie ID: %v", err)})
			return
		}

		// Determine scrapers to use
		scrapersToUse := deps.GetConfig().Scrapers.Priority
		if len(req.SelectedScrapers) > 0 {
			scrapersToUse = req.SelectedScrapers
		}

		// Scrape from sources
		results := []*models.ScraperResult{}
		for _, scraper := range deps.GetRegistry().GetByPriority(scrapersToUse) {
			result, err := scraper.Search(c.Request.Context(), parsed.ID)
			if err != nil {
				logging.Warnf("NFO comparison: %s failed: %v", scraper.Name(), err)
				continue
			}
			result.NormalizeMediaURLs()
			results = append(results, result)
		}

		if len(results) == 0 {
			c.JSON(404, ErrorResponse{Error: "No scraped data available for comparison"})
			return
		}

		// Aggregate results
		var scrapedMovie *models.Movie
		if len(req.SelectedScrapers) > 0 {
			scrapedMovie, _, err = deps.GetAggregator().AggregateWithPriority(results, req.SelectedScrapers)
		} else {
			scrapedMovie, _, err = deps.GetAggregator().Aggregate(results)
		}
		if err != nil {
			c.JSON(500, ErrorResponse{Error: fmt.Sprintf("Aggregation failed: %v", err)})
			return
		}
		response.ScrapedData = scrapedMovie

		// Step 4: Determine merge strategy using two-parameter system
		scalarStrategyStr := req.ScalarStrategy
		arrayStrategyStr := req.ArrayStrategy

		// Apply preset if specified (overrides individual strategy fields)
		if req.Preset != "" {
			var presetErr error
			scalarStrategyStr, arrayStrategyStr, presetErr = nfo.ApplyPreset(req.Preset, scalarStrategyStr, arrayStrategyStr)
			if presetErr != nil {
				c.JSON(400, ErrorResponse{Error: presetErr.Error()})
				return
			}
			logging.Infof("compareNFO: Applied preset '%s': scalar=%s, array=%s", req.Preset, scalarStrategyStr, arrayStrategyStr)
		}

		// Support backward compatibility with old merge_strategy field
		if req.MergeStrategy != "" && req.Preset == "" && scalarStrategyStr == "" {
			logging.Warnf("compareNFO: Using deprecated merge_strategy field: %s", req.MergeStrategy)
			// Map old single-parameter strategy to two-parameter system
			switch strings.ToLower(strings.TrimSpace(req.MergeStrategy)) {
			case "prefer-scraper":
				scalarStrategyStr = "prefer-scraper"
				arrayStrategyStr = "replace"
			case "prefer-nfo":
				scalarStrategyStr = "prefer-nfo"
				arrayStrategyStr = "merge"
			case "merge-arrays":
				scalarStrategyStr = "prefer-scraper"
				arrayStrategyStr = "merge"
			default:
				c.JSON(400, ErrorResponse{Error: fmt.Sprintf("Invalid merge strategy: %s", req.MergeStrategy)})
				return
			}
		}

		// Apply defaults if not specified
		if scalarStrategyStr == "" {
			scalarStrategyStr = "prefer-nfo" // default for comparison/update mode
		}
		if arrayStrategyStr == "" {
			arrayStrategyStr = "merge" // default
		}

		// Parse strategies
		scalarStrategy := nfo.ParseScalarStrategy(scalarStrategyStr)
		mergeArrays := nfo.ParseArrayStrategy(arrayStrategyStr)

		// Step 5: Merge and generate provenance
		mergeResult, err := nfo.MergeMovieMetadataWithOptions(scrapedMovie, response.NFOData, scalarStrategy, mergeArrays)
		if err != nil {
			c.JSON(500, ErrorResponse{Error: fmt.Sprintf("Merge failed: %v", err)})
			return
		}

		response.MergedData = mergeResult.Merged

		// Convert provenance to API format
		apiProvenance := make(map[string]DataSource)
		for field, source := range mergeResult.Provenance {
			var lastUpdated *string
			if source.LastUpdated != nil {
				// Create a new variable for each iteration to avoid pointer aliasing
				formatted := source.LastUpdated.Format("2006-01-02T15:04:05Z07:00")
				lastUpdated = &formatted
			}
			// Normalize keys to lowercase to match identifyDifferences and frontend expectations
			apiProvenance[strings.ToLower(field)] = DataSource{
				Source:      source.Source,
				Confidence:  source.Confidence,
				LastUpdated: lastUpdated,
			}
		}
		response.Provenance = apiProvenance

		// Convert merge stats to API format
		response.MergeStats = &MergeStatistics{
			TotalFields:       mergeResult.Stats.TotalFields,
			FromScraper:       mergeResult.Stats.FromScraper,
			FromNFO:           mergeResult.Stats.FromNFO,
			MergedArrays:      mergeResult.Stats.MergedArrays,
			ConflictsResolved: mergeResult.Stats.ConflictsResolved,
			EmptyFields:       mergeResult.Stats.EmptyFields,
		}

		// Step 6: Identify differences (for UI display)
		response.Differences = identifyDifferences(response.NFOData, scrapedMovie, mergeResult.Merged)

		c.JSON(200, response)
	}
}
