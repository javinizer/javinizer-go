package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/scraper/r18dev"
	"github.com/spf13/cobra"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/javinizer/javinizer-go/docs/swagger" // Import generated docs
)

// @title Javinizer API
// @version 1.0
// @description REST API for JAV metadata scraping and file organization
// @termsOfService https://github.com/javinizer/javinizer-go

// @contact.name API Support
// @contact.url https://github.com/javinizer/javinizer-go/issues

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host localhost:8080
// @BasePath /
// @schemes http https

func newAPICmd() *cobra.Command {
	var (
		host string
		port int
	)

	cmd := &cobra.Command{
		Use:   "api",
		Short: "Start the Javinizer API server",
		Long:  `Start a REST API server for scraping and retrieving JAV metadata`,
		Run: func(cmd *cobra.Command, args []string) {
			runAPI(host, port)
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "Server host address (default from config)")
	cmd.Flags().IntVar(&port, "port", 0, "Server port (default from config)")

	return cmd
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status   string   `json:"status" example:"ok"`
	Scrapers []string `json:"scrapers" example:"r18dev,dmm"`
}

// ScrapeRequest represents the scrape request payload
type ScrapeRequest struct {
	ID string `json:"id" binding:"required" example:"IPX-535"`
}

// ScrapeResponse represents the scrape response
type ScrapeResponse struct {
	Cached      bool          `json:"cached" example:"false"`
	Movie       *models.Movie `json:"movie"`
	SourcesUsed int           `json:"sources_used,omitempty" example:"2"`
	Errors      []string      `json:"errors,omitempty"`
}

// MovieResponse represents a movie response
type MovieResponse struct {
	Movie *models.Movie `json:"movie"`
}

// MoviesResponse represents a list of movies response
type MoviesResponse struct {
	Movies []models.Movie `json:"movies"`
	Count  int            `json:"count" example:"20"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error  string   `json:"error" example:"Movie not found"`
	Errors []string `json:"errors,omitempty"`
}

func runAPI(hostFlag string, portFlag int) {
	// Load configuration
	if err := loadConfig(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override config with flags if provided
	if hostFlag != "" {
		cfg.Server.Host = hostFlag
	}
	if portFlag != 0 {
		cfg.Server.Port = portFlag
	}

	logging.Infof("Loaded configuration from %s", cfgFile)

	// Ensure data directory exists
	dataDir := filepath.Dir(cfg.Database.DSN)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Initialize database
	db, err := database.New(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := db.AutoMigrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	logging.Info("Database initialized and migrated")

	// Initialize scrapers
	registry := models.NewScraperRegistry()
	registry.Register(r18dev.New(cfg))
	registry.Register(dmm.New(cfg))

	logging.Infof("Registered %d scrapers", len(registry.GetAll()))

	// Initialize aggregator
	agg := aggregator.New(cfg)

	// Initialize repositories
	movieRepo := database.NewMovieRepository(db)

	// Setup Gin router
	if cfg.Logging.Level != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// Scalar API documentation (modern, beautiful UI)
	router.GET("/docs", serveScalarDocs)
	router.GET("/docs/openapi.json", ginSwagger.WrapHandler(swaggerFiles.Handler,
		ginSwagger.URL("/docs/openapi.json"),
		ginSwagger.DefaultModelsExpandDepth(-1)))

	// Also provide traditional Swagger UI as fallback
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Health check endpoint
	router.GET("/health", healthCheck(registry))

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		v1.POST("/scrape", scrapeMovie(registry, agg, movieRepo))
		v1.GET("/movie/:id", getMovie(movieRepo))
		v1.GET("/movies", listMovies(movieRepo))
		v1.GET("/config", getConfig())
	}

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logging.Infof("Starting API server on %s", addr)
	logging.Infof("📚 API Documentation (Scalar): http://%s/docs", addr)
	logging.Infof("📖 Swagger UI: http://%s/swagger/index.html", addr)
	logging.Infof("🏥 Health check: http://%s/health", addr)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// serveScalarDocs serves the Scalar API documentation UI
func serveScalarDocs(c *gin.Context) {
	html := `<!doctype html>
<html>
  <head>
    <title>Javinizer API Documentation</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <script
      id="api-reference"
      data-url="/docs/openapi.json"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, html)
}

// healthCheck godoc
// @Summary Health check
// @Description Check API health and list enabled scrapers
// @Tags system
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /health [get]
func healthCheck(registry *models.ScraperRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		scrapers := []string{}
		for _, s := range registry.GetEnabled() {
			scrapers = append(scrapers, s.Name())
		}
		c.JSON(200, HealthResponse{
			Status:   "ok",
			Scrapers: scrapers,
		})
	}
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
func scrapeMovie(registry *models.ScraperRegistry, agg *aggregator.Aggregator, movieRepo *database.MovieRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ScrapeRequest

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		// Check if already in database
		existing, err := movieRepo.FindByID(req.ID)
		if err == nil && existing != nil {
			c.JSON(200, ScrapeResponse{
				Cached: true,
				Movie:  existing,
			})
			return
		}

		// Scrape from sources in priority order
		results := []*models.ScraperResult{}
		errors := []string{}

		for _, scraper := range registry.GetByPriority(cfg.Scrapers.Priority) {
			result, err := scraper.Search(req.ID)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", scraper.Name(), err))
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

		// Aggregate results
		movie, err := agg.Aggregate(results)
		if err != nil {
			c.JSON(500, ErrorResponse{Error: err.Error()})
			return
		}

		movie.OriginalFileName = req.ID

		// Save to database (upsert: create or update)
		if err := movieRepo.Upsert(movie); err != nil {
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

// getMovie godoc
// @Summary Get movie by ID
// @Description Retrieve movie metadata from cache by ID
// @Tags movies
// @Produce json
// @Param id path string true "Movie ID" example:"IPX-535"
// @Success 200 {object} MovieResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/movie/{id} [get]
func getMovie(movieRepo *database.MovieRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		movie, err := movieRepo.FindByID(id)
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
func listMovies(movieRepo *database.MovieRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 20
		offset := 0

		movies, err := movieRepo.List(limit, offset)
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

// getConfig godoc
// @Summary Get configuration
// @Description Retrieve the current server configuration
// @Tags system
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/config [get]
func getConfig() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(200, cfg)
	}
}
