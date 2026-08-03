package movie

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

var movieEmbeddedURLRe = regexp.MustCompile(`(?i)https?://[^\s/]+@`)
var movieQueryURLRe = regexp.MustCompile(`(?i)(https?://[^\s?#]+)[?#][^\s]+`)

func redactEmbeddedURLs(s string) string {
	s = movieEmbeddedURLRe.ReplaceAllString(s, "https://redacted:redacted@")
	s = movieQueryURLRe.ReplaceAllString(s, "$1")
	return s
}

func redactURLCredentials(input string) string {
	u, err := url.Parse(input)
	if err != nil {
		return "redacted"
	}
	if u.Host == "" {
		if strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https") {
			return "redacted"
		}
		return input
	}
	if u.User != nil {
		u.User = url.UserPassword("redacted", "redacted")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// scrapeMovie godoc
// @Summary Scrape movie metadata
// @Description Scrape metadata from configured sources and cache in database
// @Tags movies
// @Accept json
// @Produce json
// @Param request body contracts.ScrapeRequest true "Movie ID to scrape"
// @Success 200 {object} contracts.ScrapeResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/scrape [post]
func scrapeMovie(deps MovieDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.ScrapeRequest

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		req.ID = strings.TrimSpace(req.ID)
		if req.ID == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "id is required"})
			return
		}

		// Check if request was cancelled before starting expensive operations
		select {
		case <-c.Request.Context().Done():
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "Request cancelled by client"})
			return
		default:
		}

		// Build ScrapeCmd with RawInput — the seam resolves URL/manual input
		// internally via matcher.ParseInput and CalculateOptimalScrapers.
		// Selected scrapers are passed as-is; the seam handles URL filtering.
		cmd := scrape.ScrapeCmd{
			RawInput:         req.ID,
			ForceRefresh:     req.Force,
			SelectedScrapers: req.SelectedScrapers,
		}

		// Execute scrape via the canonical Workflow seam (ADR-0001)
		wf := deps.getWorkflow()
		if wf == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "workflow not available"})
			return
		}
		scrapeCtx := c.Request.Context()
		if deps.RequestTimeoutFn != nil {
			if rt := deps.RequestTimeoutFn(); rt > 0 {
				var cancel context.CancelFunc
				scrapeCtx, cancel = context.WithTimeout(scrapeCtx, rt)
				defer cancel()
			}
		}
		result, _, err := wf.Scrape(scrapeCtx, cmd)
		if scrapeCtx.Err() != nil {
			if deps.HistoryRepo != nil && !errors.Is(scrapeCtx.Err(), context.Canceled) {
				auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer auditCancel()
				safeInput := redactURLCredentials(cmd.RawInput)
				_ = deps.HistoryRepo.Create(auditCtx, &models.History{
					MovieID:      safeInput,
					Operation:    models.HistoryOpScrape,
					OriginalPath: safeInput,
					Status:       models.HistoryStatusFailed,
					ErrorMessage: "scrape timed out",
				})
			}
			c.JSON(http.StatusGatewayTimeout, contracts.ErrorResponse{Error: "scrape timed out"})
			return
		}
		if err != nil {
			if deps.HistoryRepo != nil && !errors.Is(err, context.Canceled) {
				auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer auditCancel()
				safeInput := redactURLCredentials(cmd.RawInput)
				_ = deps.HistoryRepo.Create(auditCtx, &models.History{
					MovieID:      safeInput,
					Operation:    models.HistoryOpScrape,
					OriginalPath: safeInput,
					Status:       models.HistoryStatusFailed,
					ErrorMessage: redactEmbeddedURLs(err.Error()),
				})
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		if result.Status == scrape.StatusFailed {
			errMsg := "Movie not found"
			if result.Message != "" {
				errMsg = redactEmbeddedURLs(result.Message)
			}
			if deps.HistoryRepo != nil {
				auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer auditCancel()
				safeInput := redactURLCredentials(cmd.RawInput)
				movieID := safeInput
				for _, sr := range result.ScraperResults {
					if sr.ID != "" {
						movieID = sr.ID
						break
					}
				}
				_ = deps.HistoryRepo.Create(auditCtx, &models.History{
					MovieID:      movieID,
					Operation:    models.HistoryOpScrape,
					OriginalPath: safeInput,
					Status:       models.HistoryStatusFailed,
					ErrorMessage: errMsg,
				})
			}
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: errMsg})
			return
		}

		if deps.HistoryRepo != nil && result.Movie != nil {
			auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer auditCancel()
			_ = deps.HistoryRepo.Create(auditCtx, &models.History{
				MovieID:      result.Movie.ID,
				Operation:    models.HistoryOpScrape,
				OriginalPath: redactURLCredentials(cmd.RawInput),
				Status:       models.HistoryStatusSuccess,
			})
		}

		// Generate temp poster for the scraped movie.
		// Poster generation was moved out of the scrape orchestrator;
		// API endpoints that call wf.Scrape() directly must do it explicitly.
		// Serialize per (sentinel-job, movie ID): DownloadFromURL replaces the
		// cached -full.jpg non-atomically (Remove before Rename), so two
		// concurrent single-movie scrapes resolving to the same ID must not
		// interleave (parity with the batch scrape phase's L2 lock).
		var posterErrs []string
		if deps.PosterGen != nil && result.Movie != nil {
			// The release is deferred immediately after acquisition: the lock
			// is refcounted (worker.AcquirePosterSourceLock), so a recovered
			// panic inside GeneratePoster must still release it — an explicit
			// release below would leak the refcount entry, permanently
			// deadlocking the key (L1).
			release := worker.AcquirePosterSourceLock(models.SentinelJobID().String(), result.Movie.ID)
			defer release()
			// A failed generation must NOT be discarded: the endpoint would
			// otherwise report success while the poster cache holds no image.
			// The scrape itself succeeded, so surface it as a movie-level warning
			// (ScrapeResponse.Errors) with credential material redacted, parity
			// with the history-audit redaction above.
			if genErr := deps.PosterGen.GeneratePoster(c.Request.Context(), models.SentinelJobID().String(), result.Movie); genErr != nil {
				logging.Errorf("Poster generation failed for %s after scrape: %v", result.Movie.ID, genErr)
				posterErrs = append(posterErrs, fmt.Sprintf("poster generation failed: %s", redactEmbeddedURLs(genErr.Error())))
			}
		}

		// Cached if the scrape seam served from the movie DB cache. Read the
		// explicit flag rather than inferring from ScraperResults length, since
		// cache hits now populate ScraperResults (synthesized) for the source viewer.
		cached := result.Cached
		// SourcesUsed reports live scrapers consulted. On a cache hit the
		// single ScraperResults entry is synthesized from the cached movie, not
		// a real scraper, so report 0.
		sourcesUsed := len(result.ScraperResults)
		if cached {
			sourcesUsed = 0
		}

		c.JSON(http.StatusOK, contracts.ScrapeResponse{
			Cached:      cached,
			Movie:       contracts.MovieViewFromModel(result.Movie),
			SourcesUsed: sourcesUsed,
			Errors:      posterErrs,
		})
	}
}

// getMovie godoc
// @Summary Get movie by ID
// @Description Retrieve movie metadata from cache by ID, optionally with provenance information
// @Tags movies
// @Produce json
// @Param id path string true "Movie ID" example:"IPX-535"
// @Success 200 {object} contracts.MovieResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/movies/{id} [get]
func getMovie(deps MovieDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		movie, err := deps.FindByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Movie not found"})
			return
		}

		response := contracts.MovieResponse{Movie: contracts.MovieViewFromModel(movie)}

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
// @Success 200 {object} contracts.MoviesResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/movies [get]
func listMovies(deps MovieDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, offset := core.ParsePagination(c, 20, 500)

		movies, err := deps.List(c.Request.Context(), limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, contracts.MoviesResponse{
			Movies: contracts.MovieViewSliceFromModels(movies),
			Count:  len(movies),
		})
	}
}
