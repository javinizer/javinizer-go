package movie

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// rescrapeMovie godoc
// @Summary Rescrape movie with specific scrapers
// @Description Rescrape movie metadata using selected scrapers only
// @Tags movies
// @Accept json
// @Produce json
// @Param id path string true "Movie ID" example:"IPX-535"
// @Param request body contracts.RescrapeRequest true "Rescrape options"
// @Success 200 {object} contracts.MovieResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/movies/{id}/rescrape [post]
func rescrapeMovie(deps MovieDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		movieID := c.Param("id")

		var req contracts.RescrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		if len(req.SelectedScrapers) == 0 {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "selected_scrapers cannot be empty"})
			return
		}

		logging.Infof("API rescrape request for %s with scrapers: %v", movieID, req.SelectedScrapers)

		cmd := scrape.ScrapeCmd{
			MovieID:          movieID,
			ForceRefresh:     req.Force,
			SelectedScrapers: req.SelectedScrapers,
		}

		// Execute rescrape via the canonical Workflow seam (ADR-0001)
		wf := deps.getWorkflow()
		if wf == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "workflow not available"})
			return
		}
		result, _, err := wf.Scrape(c.Request.Context(), cmd, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		if result.Status == scrape.StatusFailed {
			errMsg := "No results from selected scrapers"
			if result.Message != "" {
				errMsg = result.Message
			}
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: errMsg})
			return
		}

		// Generate temp poster for the rescraped movie.
		// Poster generation was moved out of the scrape orchestrator;
		// API endpoints that call wf.Scrape() directly must do it explicitly.
		if deps.PosterGen != nil && result.Movie != nil {
			_ = deps.PosterGen.GeneratePoster(c.Request.Context(), models.SentinelJobID().String(), result.Movie)
		}

		c.JSON(http.StatusOK, contracts.MovieResponse{Movie: contracts.MovieViewFromModel(result.Movie)})
	}
}
