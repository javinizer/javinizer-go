package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
)

// Sentinel errors for NFO validation
var (
	ErrNFONotFound     = errors.New("nfo file not found")
	ErrNFOAccessDenied = errors.New("access denied: path is outside allowed directories")
	ErrNFOInvalidPath  = errors.New("invalid file path")
	ErrNFOIsDirectory  = errors.New("path is a directory, not a file")
)

// validateNFOPath validates an NFO file path against security constraints
// Returns the validated absolute path or a sentinel error
func validateNFOPath(requestedPath string, allowedDirs []string) (string, error) {
	// Expand ~ in requested path (security: consistent with allowlist handling)
	expandedPath := requestedPath
	if strings.HasPrefix(requestedPath, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if requestedPath == "~" {
				expandedPath = home
			} else if strings.HasPrefix(requestedPath, "~/") {
				expandedPath = filepath.Join(home, strings.TrimPrefix(requestedPath, "~/"))
			}
		}
	}

	// Clean and normalize the path
	cleanPath := filepath.Clean(expandedPath)

	// Convert to absolute path
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", ErrNFOInvalidPath
	}

	// Resolve symlinks to prevent symlink attacks
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// Path doesn't exist or can't be resolved
		if os.IsNotExist(err) {
			return "", ErrNFONotFound
		}
		return "", ErrNFOInvalidPath
	}

	// Verify it's a regular file (not a directory)
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", ErrNFONotFound
	}
	if info.IsDir() {
		return "", ErrNFOIsDirectory
	}

	// Security: Deny by default when allowedDirs is empty to prevent arbitrary file access
	// Operators must explicitly configure allowed directories in config
	if len(allowedDirs) == 0 {
		return "", ErrNFOAccessDenied
	}

	// Check if resolved path is within one of the allowed directories
	{
		allowed := false
		for _, allowedDir := range allowedDirs {
			// Expand tilde (~) to user home directory
			if strings.HasPrefix(allowedDir, "~") {
				if home, err := os.UserHomeDir(); err == nil {
					if allowedDir == "~" {
						// Bare tilde expands to home directory
						allowedDir = home
					} else if strings.HasPrefix(allowedDir, "~/") {
						// Tilde with path expands to home + path
						allowedDir = filepath.Join(home, strings.TrimPrefix(allowedDir, "~/"))
					}
					// Note: "~otheruser" format is not supported
				}
			}

			// Clean and normalize the allowed directory path
			allowedDir = filepath.Clean(allowedDir)

			// Resolve allowed directory to handle symlinks
			absAllowedDir, err := filepath.Abs(allowedDir)
			if err != nil {
				continue
			}
			resolvedAllowedDir, err := filepath.EvalSymlinks(absAllowedDir)
			if err != nil {
				// If allowed dir doesn't exist, skip it
				continue
			}

			// Check if resolved path is within this allowed directory
			// Use filepath.Rel to check if path is under allowed directory
			rel, err := filepath.Rel(resolvedAllowedDir, resolvedPath)
			if err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
				allowed = true
				break
			}
		}

		if !allowed {
			return "", ErrNFOAccessDenied
		}
	}

	return resolvedPath, nil
}

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
		parsed, err := matcher.ParseInput(movieID)
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
			result, err := scraper.Search(parsed.ID)
			if err != nil {
				logging.Warnf("NFO comparison: %s failed: %v", scraper.Name(), err)
				continue
			}
			results = append(results, result)
		}

		if len(results) == 0 {
			c.JSON(404, ErrorResponse{Error: "No scraped data available for comparison"})
			return
		}

		// Aggregate results
		var scrapedMovie *models.Movie
		if len(req.SelectedScrapers) > 0 {
			scrapedMovie, err = deps.GetAggregator().AggregateWithPriority(results, req.SelectedScrapers)
		} else {
			scrapedMovie, err = deps.GetAggregator().Aggregate(results)
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

// identifyDifferences compares NFO, scraped, and merged data to identify key differences
func identifyDifferences(nfoMovie, scrapedMovie, mergedMovie *models.Movie) []FieldDifference {
	diffs := []FieldDifference{}

	// Compare basic string fields
	if nfoMovie.Title != scrapedMovie.Title {
		diffs = append(diffs, FieldDifference{
			Field:        "title",
			NFOValue:     nfoMovie.Title,
			ScrapedValue: scrapedMovie.Title,
			MergedValue:  mergedMovie.Title,
		})
	}

	if nfoMovie.Description != scrapedMovie.Description {
		diffs = append(diffs, FieldDifference{
			Field:        "description",
			NFOValue:     nfoMovie.Description,
			ScrapedValue: scrapedMovie.Description,
			MergedValue:  mergedMovie.Description,
		})
	}

	if nfoMovie.Director != scrapedMovie.Director {
		diffs = append(diffs, FieldDifference{
			Field:        "director",
			NFOValue:     nfoMovie.Director,
			ScrapedValue: scrapedMovie.Director,
			MergedValue:  mergedMovie.Director,
		})
	}

	if nfoMovie.Maker != scrapedMovie.Maker {
		diffs = append(diffs, FieldDifference{
			Field:        "maker",
			NFOValue:     nfoMovie.Maker,
			ScrapedValue: scrapedMovie.Maker,
			MergedValue:  mergedMovie.Maker,
		})
	}

	// Compare numeric fields
	if nfoMovie.Runtime != scrapedMovie.Runtime {
		diffs = append(diffs, FieldDifference{
			Field:        "runtime",
			NFOValue:     nfoMovie.Runtime,
			ScrapedValue: scrapedMovie.Runtime,
			MergedValue:  mergedMovie.Runtime,
		})
	}

	// Compare array lengths as a proxy for content differences
	if len(nfoMovie.Actresses) != len(scrapedMovie.Actresses) {
		diffs = append(diffs, FieldDifference{
			Field:        "actresses",
			NFOValue:     fmt.Sprintf("%d actresses", len(nfoMovie.Actresses)),
			ScrapedValue: fmt.Sprintf("%d actresses", len(scrapedMovie.Actresses)),
			MergedValue:  fmt.Sprintf("%d actresses", len(mergedMovie.Actresses)),
		})
	}

	if len(nfoMovie.Genres) != len(scrapedMovie.Genres) {
		diffs = append(diffs, FieldDifference{
			Field:        "genres",
			NFOValue:     fmt.Sprintf("%d genres", len(nfoMovie.Genres)),
			ScrapedValue: fmt.Sprintf("%d genres", len(scrapedMovie.Genres)),
			MergedValue:  fmt.Sprintf("%d genres", len(mergedMovie.Genres)),
		})
	}

	return diffs
}
