package movie

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

type scraperPanicError struct {
	message string
}

func (e *scraperPanicError) Error() string {
	return e.message
}

func isScraperPanicError(err error) bool {
	_, ok := err.(*scraperPanicError)
	return ok
}

func safeScrapeURL(c *gin.Context, scraper models.DirectURLScraper, url string) (result *models.ScraperResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &scraperPanicError{message: fmt.Sprintf("scraper panic during direct URL scrape: %v", r)}
		}
	}()
	return scraper.ScrapeURL(c.Request.Context(), url)
}

func safeSearch(c *gin.Context, scraper models.Scraper, id string) (result *models.ScraperResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &scraperPanicError{message: fmt.Sprintf("scraper panic during search: %v", r)}
		}
	}()
	return scraper.Search(c.Request.Context(), id)
}

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
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		req.ID = strings.TrimSpace(req.ID)

		logging.Infof("API scrape request for ID: %s, Force: %v, Custom scrapers: %v",
			req.ID, req.Force, req.SelectedScrapers)

		parsed, err := matcher.ParseInput(req.ID, deps.GetRegistry())
		if err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		if parsed.IsURL {
			logging.Infof("URL detected: %s (extracted ID: %s, scraper hint: %s)", req.ID, parsed.ID, parsed.ScraperHint)
		}

		scrapersToUse := matcher.CalculateOptimalScrapers(
			req.SelectedScrapers,
			deps.GetConfig().Scrapers.Priority,
			parsed,
		)

		// Log scraper selection for URL inputs
		if parsed.IsURL && len(parsed.CompatibleScrapers) > 0 {
			if len(req.SelectedScrapers) > 0 {
				logging.Infof("URL detected, filtered scrapers from %v to URL-compatible: %v", req.SelectedScrapers, scrapersToUse)
			} else if parsed.ScraperHint != "" {
				logging.Infof("URL detected, using compatible scrapers with %s prioritized: %v", parsed.ScraperHint, scrapersToUse)
			} else {
				logging.Infof("URL detected, using URL-compatible scrapers: %v", scrapersToUse)
			}
		} else if len(req.SelectedScrapers) > 0 {
			logging.Infof("Using custom scrapers from request: %v", scrapersToUse)
		}

		// Warn if URL detected but no compatible scrapers
		if parsed.IsURL && len(parsed.CompatibleScrapers) == 0 {
			logging.Warnf("URL detected but no registered scrapers can handle it. Input may fail to scrape.")
		}

		// Check if request was cancelled before starting expensive operations
		select {
		case <-c.Request.Context().Done():
			c.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "Request cancelled by client"})
			return
		default:
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
				c.JSON(http.StatusOK, ScrapeResponse{
					Cached: true,
					Movie:  movie,
				})
				return
			}
		}

		// Scrape from sources in priority order - use getters for thread-safe access
		results := []*models.ScraperResult{}
		scrapeErrors := []string{}
		anyPanic := false

		for _, scraper := range deps.GetRegistry().GetByPriority(scrapersToUse) {
			select {
			case <-c.Request.Context().Done():
				c.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "Request cancelled by client"})
				return
			default:
			}

			logging.Infof("Scraping from %s...", scraper.Name())

			var result *models.ScraperResult
			var err error

			if parsed.IsURL {
				if handler, ok := scraper.(models.URLHandler); ok && handler.CanHandleURL(req.ID) {
					if directScraper, ok := scraper.(models.DirectURLScraper); ok {
						logging.Debugf("Trying direct URL scrape for %s", scraper.Name())
						result, err = safeScrapeURL(c, directScraper, req.ID)
						if err == nil {
							logging.Debugf("Direct URL scrape succeeded for %s", scraper.Name())
							result.NormalizeMediaURLs()
							results = append(results, result)
							continue
						}
						if isScraperPanicError(err) {
							anyPanic = true
							logging.Errorf("Scraper %s panicked during direct URL scrape: %v", scraper.Name(), err)
							scrapeErrors = append(scrapeErrors, fmt.Sprintf("%s (direct URL): %v", scraper.Name(), err))
						} else if scraperErr, ok := models.AsScraperError(err); ok {
							scrapeErrors = append(scrapeErrors, fmt.Sprintf("%s (direct URL): %v", scraper.Name(), scraperErr))
							logging.Debugf("Direct URL scrape failed for %s (%s), falling back to ID search", scraper.Name(), scraperErr.Kind)
						} else {
							scrapeErrors = append(scrapeErrors, fmt.Sprintf("%s (direct URL): %v", scraper.Name(), err))
							logging.Debugf("Direct URL scrape failed for %s: %v, falling back to ID search", scraper.Name(), err)
						}
					}
				}
			}

			result, err = safeSearch(c, scraper, parsed.ID)
			if err != nil {
				if isScraperPanicError(err) {
					anyPanic = true
					logging.Errorf("Scraper %s panicked: %v", scraper.Name(), err)
				} else {
					logging.Warnf("%s: %v", scraper.Name(), err)
				}
				scrapeErrors = append(scrapeErrors, fmt.Sprintf("%s: %v", scraper.Name(), err))
				continue
			}
			result.NormalizeMediaURLs()
			results = append(results, result)
		}

		if len(results) == 0 {
			if anyPanic {
				c.JSON(http.StatusInternalServerError, ErrorResponse{
					Error:  "Internal scraper error - one or more scrapers crashed",
					Errors: scrapeErrors,
				})
				return
			}
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:  "Movie not found",
				Errors: scrapeErrors,
			})
			return
		}

		// Aggregate results - use the actual scrapers that were queried
		// If user explicitly selected scrapers, use those (filtered to URL-compatible if URL)
		// Otherwise use default aggregation
		var movie *models.Movie
		if len(req.SelectedScrapers) > 0 {
			logging.Infof("Aggregating with custom priority: %v", scrapersToUse)
			movie, err = deps.GetAggregator().AggregateWithPriority(results, scrapersToUse)
		} else {
			movie, err = deps.GetAggregator().Aggregate(results)
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		movie.OriginalFileName = parsed.ID

		// Save to database (upsert: create or update)
		if _, err := deps.MovieRepo.Upsert(movie); err != nil {
			logging.Errorf("Failed to save movie to database: %v", err)
		}

		c.JSON(http.StatusOK, ScrapeResponse{
			Cached:      false,
			Movie:       movie,
			SourcesUsed: len(results),
			Errors:      scrapeErrors,
		})
	}
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
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Movie not found"})
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

		c.JSON(http.StatusOK, response)
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
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, MoviesResponse{
			Movies: movies,
			Count:  len(movies),
		})
	}
}
