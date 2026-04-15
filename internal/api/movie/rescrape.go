package movie

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

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
// @Router /api/v1/movies/{id}/rescrape [post]
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
		scrapeErrors := []string{}

		for _, scraper := range deps.GetRegistry().GetByPriority(req.SelectedScrapers) {
			logging.Infof("Rescraping from %s...", scraper.Name())
			result, err := scraper.Search(c.Request.Context(), movieID)
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
				Error:  "No results from selected scrapers",
				Errors: scrapeErrors,
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
		if _, err := deps.MovieRepo.Upsert(movie); err != nil {
			logging.Warnf("Failed to save movie to DB: %v", err)
		}

		c.JSON(200, MovieResponse{Movie: movie})
	}
}
