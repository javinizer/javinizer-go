package api

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
		errors := []string{}

		for _, scraper := range deps.GetRegistry().GetByPriority(scrapersToUse) {
			logging.Infof("Scraping from %s...", scraper.Name())
			result, err := scraper.Search(parsed.ID)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", scraper.Name(), err))
				logging.Warnf("%s: %v", scraper.Name(), err)
				continue
			}
			results = append(results, result)
		}

		if len(results) == 0 {
			c.JSON(404, ErrorResponse{
				Error:  "Movie not found",
				Errors: errors,
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
			Errors:      errors,
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
// @Description Retrieve movie metadata from cache by ID
// @Tags movies
// @Produce json
// @Param id path string true "Movie ID" example:"IPX-535"
// @Success 200 {object} MovieResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/movie/{id} [get]
func getMovie(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		movie, err := deps.MovieRepo.FindByID(id)
		if err != nil {
			c.JSON(404, ErrorResponse{Error: "Movie not found"})
			return
		}

		c.JSON(200, MovieResponse{Movie: movie})
	}
}

// listMovies godoc
// @Summary List cached movies
// @Description Get a list of cached movies from the database
// @Tags movies
// @Produce json
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

// rescrapeMovie godoc
// @Summary Rescrape movie with specific scrapers
// @Description Rescrape movie metadata using selected scrapers only
// @Tags movies
// @Accept json
// @Produce json
// @Param id path string true "Movie ID" example:"IPX-535"
// @Param request body RescrapeRequest true "Rescrape options"
// @Success 200 {object} MovieResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/movie/{id}/rescrape [post]
func rescrapeMovie(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		movieID := c.Param("id")

		var req RescrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		if len(req.SelectedScrapers) == 0 {
			c.JSON(400, ErrorResponse{Error: "selected_scrapers cannot be empty"})
			return
		}

		logging.Infof("API rescrape request for %s with scrapers: %v", movieID, req.SelectedScrapers)

		// Clear cache if force
		if req.Force {
			if err := deps.MovieRepo.Delete(movieID); err != nil {
				logging.Debugf("Failed to delete %s from cache: %v", movieID, err)
			}
		}

		// Scrape with selected scrapers only
		results := []*models.ScraperResult{}
		errors := []string{}

		for _, scraper := range deps.GetRegistry().GetByPriority(req.SelectedScrapers) {
			logging.Infof("Rescraping from %s...", scraper.Name())
			result, err := scraper.Search(movieID)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", scraper.Name(), err))
				logging.Warnf("%s: %v", scraper.Name(), err)
				continue
			}
			results = append(results, result)
		}

		if len(results) == 0 {
			c.JSON(404, ErrorResponse{
				Error:  "No results from selected scrapers",
				Errors: errors,
			})
			return
		}

		// Aggregate using custom priority order
		logging.Infof("Aggregating results with custom priority: %v", req.SelectedScrapers)
		movie, err := deps.GetAggregator().AggregateWithPriority(results, req.SelectedScrapers)
		if err != nil {
			c.JSON(500, ErrorResponse{Error: err.Error()})
			return
		}

		movie.OriginalFileName = movieID

		// Save to DB
		if err := deps.MovieRepo.Upsert(movie); err != nil {
			logging.Warnf("Failed to save movie to DB: %v", err)
		}

		c.JSON(200, MovieResponse{Movie: movie})
	}
}
