package movie

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

// scrapeMovie godoc
// @Summary Scrape movie metadata
// @Description Scrape metadata from configured sources and cache in database
// @Tags movies
// @Accept json
// @Produce json
// @Param request body ScrapeRequest true "Movie ID to scrape"
// @Success 200 {object} ScrapeResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/scrape [post]
func scrapeMovie(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ScrapeRequest

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		logging.Infof("API scrape request for ID: %s, Force: %v, Custom scrapers: %v",
			req.ID, req.Force, req.SelectedScrapers)

		// Parse input (might be URL)
		parsed, err := matcher.ParseInput(req.ID)
		if err != nil {
			c.JSON(400, ErrorResponse{Error: fmt.Sprintf("Invalid input: %v", err)})
			return
		}

		// Determine scraper list
		scrapersToUse := deps.GetConfig().Scrapers.Priority
		if len(req.SelectedScrapers) > 0 {
			scrapersToUse = req.SelectedScrapers
			logging.Infof("Using custom scrapers from request: %v", scrapersToUse)
		} else if parsed.IsURL && parsed.ScraperHint != "" {
			// Only auto-prioritize scraper hint if user didn't provide custom scrapers
			scrapersToUse = reorderWithPriority(scrapersToUse, parsed.ScraperHint)
			logging.Infof("URL detected, prioritized %s scraper", parsed.ScraperHint)
		}

		// Clear cache if custom scrapers or force
		usingCustomScrapers := len(req.SelectedScrapers) > 0
		if usingCustomScrapers || req.Force {
			if err := deps.MovieRepo.Delete(parsed.ID); err != nil {
				logging.Debugf("Failed to delete %s from cache: %v", parsed.ID, err)
			} else {
				logging.Infof("Cache cleared for %s", parsed.ID)
			}
		}

		// Skip cache if custom scrapers
		if !usingCustomScrapers && !req.Force {
			if movie, err := deps.MovieRepo.FindByID(parsed.ID); err == nil {
				logging.Info("Found in cache!")
				c.JSON(200, ScrapeResponse{
					Cached: true,
					Movie:  movie,
				})
				return
			}
		}

		// Scrape from sources in priority order - use getters for thread-safe access
		results := []*models.ScraperResult{}
		scrapeErrors := []string{}

		for _, scraper := range deps.GetRegistry().GetByPriority(scrapersToUse) {
			logging.Infof("Scraping from %s...", scraper.Name())
			result, err := scraper.Search(parsed.ID)
			if err != nil {
				scrapeErrors = append(scrapeErrors, fmt.Sprintf("%s: %v", scraper.Name(), err))
				logging.Warnf("%s: %v", scraper.Name(), err)
				continue
			}
			result.NormalizeMediaURLs()
			results = append(results, result)
		}

		if len(results) == 0 {
			c.JSON(404, ErrorResponse{
				Error:  "Movie not found",
				Errors: scrapeErrors,
			})
			return
		}

		// Aggregate results - use custom priority if provided, otherwise use config priority
		var movie *models.Movie
		if len(req.SelectedScrapers) > 0 {
			logging.Infof("Aggregating with custom priority: %v", req.SelectedScrapers)
			movie, err = deps.GetAggregator().AggregateWithPriority(results, req.SelectedScrapers)
		} else {
			movie, err = deps.GetAggregator().Aggregate(results)
		}
		if err != nil {
			c.JSON(500, ErrorResponse{Error: err.Error()})
			return
		}

		movie.OriginalFileName = parsed.ID

		// Save to database (upsert: create or update)
		if err := deps.MovieRepo.Upsert(movie); err != nil {
			logging.Errorf("Failed to save movie to database: %v", err)
		}

		c.JSON(200, ScrapeResponse{
			Cached:      false,
			Movie:       movie,
			SourcesUsed: len(results),
			Errors:      scrapeErrors,
		})
	}
}

// reorderWithPriority moves priority scraper to front of list
func reorderWithPriority(scrapers []string, priority string) []string {
	result := []string{priority}
	for _, s := range scrapers {
		if s != priority {
			result = append(result, s)
		}
	}
	return result
}

// getMovie godoc
// @Summary Get movie by ID
// @Description Retrieve movie metadata from cache by ID, optionally with provenance information
// @Tags movies
// @Produce json
// @Param id path string true "Movie ID" example:"IPX-535"
// @Param include_provenance query bool false "Include field-level provenance data" example:"false"
// @Success 200 {object} MovieResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/movies/{id} [get]
func getMovie(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		includeProvenance := c.Query("include_provenance") == "true"

		movie, err := deps.MovieRepo.FindByID(id)
		if err != nil {
			c.JSON(404, ErrorResponse{Error: "Movie not found"})
			return
		}

		response := MovieResponse{Movie: movie}

		// If provenance requested, try to generate it by comparing with NFO (if exists)
		// Note: This is a best-effort since we don't persist provenance in the database
		if includeProvenance {
			// For provenance to be meaningful, we'd need to know the original NFO path
			// Since we don't track that in the database currently, this feature is limited
			// It will be more useful in the context of batch operations where we have file paths
			logging.Debugf("Provenance requested for movie %s, but no file context available", id)
		}

		c.JSON(200, response)
	}
}

// listMovies godoc
// @Summary List cached movies
// @Description Get a paginated list of all movies cached in the database. Supports pagination via limit and offset query parameters. Returns movie count and basic metadata.
// @Tags movies
// @Produce json
// @Param limit query int false "Max number of movies to return" example:"20"
// @Param offset query int false "Number of movies to skip" example:"0"
// @Success 200 {object} MoviesResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/movies [get]
func listMovies(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 20
		offset := 0

		movies, err := deps.MovieRepo.List(limit, offset)
		if err != nil {
			c.JSON(500, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(200, MoviesResponse{
			Movies: movies,
			Count:  len(movies),
		})
	}
}
