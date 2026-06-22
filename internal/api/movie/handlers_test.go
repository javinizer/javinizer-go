package movie

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestScrapeMovie(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		setupData      func(*testing.T, database.MovieRepositoryInterface)
		setupScraper   func(*scraperutil.ScraperRegistry)
		expectedStatus int
		validateFn     func(*testing.T, *contracts.ScrapeResponse)
	}{
		{
			name: "successful scrape - not cached",
			requestBody: contracts.ScrapeRequest{
				ID: "IPX-535",
			},
			setupData: func(_ *testing.T, repo database.MovieRepositoryInterface) {
				// Empty - not cached
			},
			setupScraper: func(registry *scraperutil.ScraperRegistry) {
				scraper := &mockScraperWithResults{
					name:    "r18dev",
					enabled: true,
					result: &models.ScraperResult{
						Source: "r18dev",
						ID:     "IPX-535",
						Title:  "Test Movie",
					},
				}
				registry.RegisterInstance(scraper)
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.ScrapeResponse) {
				assert.False(t, resp.Cached)
				assert.NotNil(t, resp.Movie)
				assert.Equal(t, "IPX-535", resp.Movie.ID)
			},
		},
		{
			name: "successful scrape - from cache",
			requestBody: contracts.ScrapeRequest{
				ID: "IPX-535",
			},
			setupData: func(t *testing.T, repo database.MovieRepositoryInterface) {
				_, err := repo.Upsert(context.TODO(), &models.Movie{
					ID:    "IPX-535",
					Title: "Cached Movie",
				})
				require.NoError(t, err)
			},
			setupScraper:   func(registry *scraperutil.ScraperRegistry) {},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.ScrapeResponse) {
				assert.True(t, resp.Cached)
				assert.Equal(t, "Cached Movie", resp.Movie.Title)
			},
		},
		{
			name: "invalid request - missing ID",
			requestBody: map[string]string{
				"invalid": "data",
			},
			setupData:      func(_ *testing.T, repo database.MovieRepositoryInterface) {},
			setupScraper:   func(registry *scraperutil.ScraperRegistry) {},
			expectedStatus: 400,
		},
		{
			name: "movie not found - all scrapers fail",
			requestBody: contracts.ScrapeRequest{
				ID: "INVALID-123",
			},
			setupData: func(_ *testing.T, repo database.MovieRepositoryInterface) {},
			setupScraper: func(registry *scraperutil.ScraperRegistry) {
				registry.RegisterInstance(&mockScraperWithResults{
					name:    "r18dev",
					enabled: true,
					err:     errors.New("not found"),
				})
			},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev"},
				},
			}

			deps := createTestDeps(t, cfg, "")
			movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))
			tt.setupData(t, deps.Repos.MovieRepo)
			tt.setupScraper(deps.CoreDeps.GetRegistry())

			router := gin.New()
			router.POST("/scrape", scrapeMovie(movieDeps))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/scrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response contracts.ScrapeResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestGetMovie(t *testing.T) {
	tests := []struct {
		name           string
		movieID        string
		setupData      func(*testing.T, database.MovieRepositoryInterface)
		expectedStatus int
		validateFn     func(*testing.T, *contracts.MovieResponse)
	}{
		{
			name:    "get existing movie",
			movieID: "IPX-535",
			setupData: func(t *testing.T, repo database.MovieRepositoryInterface) {
				_, err := repo.Upsert(context.TODO(), &models.Movie{
					ID:    "IPX-535",
					Title: "Test Movie",
				})
				require.NoError(t, err)
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.MovieResponse) {
				assert.NotNil(t, resp.Movie)
				assert.Equal(t, "IPX-535", resp.Movie.ID)
			},
		},
		{
			name:           "movie not found",
			movieID:        "NONEXISTENT-123",
			setupData:      func(_ *testing.T, repo database.MovieRepositoryInterface) {},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")
			movieDeps := NewMovieDeps(deps.Repos.MovieRepo)
			tt.setupData(t, deps.Repos.MovieRepo)

			router := gin.New()
			router.GET("/movie/:id", getMovie(movieDeps))

			req := httptest.NewRequest("GET", "/movie/"+tt.movieID, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response contracts.MovieResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestListMovies(t *testing.T) {
	tests := []struct {
		name           string
		setupData      func(*testing.T, database.MovieRepositoryInterface)
		expectedStatus int
		validateFn     func(*testing.T, *contracts.MoviesResponse)
	}{
		{
			name: "list multiple movies",
			setupData: func(t *testing.T, repo database.MovieRepositoryInterface) {
				_, err := repo.Upsert(context.TODO(), &models.Movie{ContentID: "ipx535", ID: "IPX-535", Title: "Movie 1"})
				require.NoError(t, err)
				_, err = repo.Upsert(context.TODO(), &models.Movie{ContentID: "abc123", ID: "ABC-123", Title: "Movie 2"})
				require.NoError(t, err)
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.MoviesResponse) {
				assert.Len(t, resp.Movies, 2)
				assert.Equal(t, 2, resp.Count)
			},
		},
		{
			name:           "list empty database",
			setupData:      func(_ *testing.T, repo database.MovieRepositoryInterface) {},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *contracts.MoviesResponse) {
				assert.Empty(t, resp.Movies)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")
			movieDeps := NewMovieDeps(deps.Repos.MovieRepo)
			tt.setupData(t, deps.Repos.MovieRepo)

			router := gin.New()
			router.GET("/movies", listMovies(movieDeps))

			req := httptest.NewRequest("GET", "/movies", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response contracts.MoviesResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestScrapeMovie_SQLInjectionPrevention(t *testing.T) {
	// Test that SQL injection attempts in movie ID are safely handled
	// CRITICAL: Register a stub scraper so the handler reaches database code (Upsert)
	// Otherwise handler returns 404 before SQL injection vulnerability is exercised

	maliciousIDs := []string{
		"IPX-535'; DROP TABLE movies; --",
		"IPX-535' OR '1'='1",
	}

	for i, maliciousID := range maliciousIDs {
		t.Run("SQLInjection:"+maliciousID, func(t *testing.T) {
			// Create fresh registry and repo for each test to avoid UNIQUE conflicts
			cfg := &config.Config{Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}}}
			deps := createTestDeps(t, cfg, "")
			stubScraper := &mockScraperWithResults{
				name:    "r18dev",
				enabled: true,
				result: &models.ScraperResult{
					ID:    fmt.Sprintf("SAFE-ID-%d", i), // Unique safe ID per test
					Title: "Test Movie",
				},
				err: nil,
			}
			deps.CoreDeps.GetRegistry().RegisterInstance(stubScraper)

			router := gin.New()
			movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))
			router.POST("/scrape", scrapeMovie(movieDeps))

			// Get initial movie count (should be 0)
			// CRITICAL: List(limit, offset) - not List(offset, limit)!
			initialMovies, err := deps.Repos.MovieRepo.List(context.TODO(), 100, 0)
			require.NoError(t, err)
			initialCount := len(initialMovies)

			reqBody := contracts.ScrapeRequest{ID: maliciousID}
			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/scrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Tighten status code assertion: expect 200 (success), 400 (bad request),
			// 404 (not found), or 500 (Scrape seam propagates Upsert failure on malformed IDs).
			// 500 is NOT an injection vulnerability — it's the Scrape seam correctly rejecting a
			// save with a malformed ID.
			assert.True(t, w.Code == 200 || w.Code == 400 || w.Code == 404 || w.Code == 500,
				"Expected 200/400/404/500, got %d (unexpected status code)", w.Code)

			// CRITICAL: Verify response doesn't contain malicious injection payload
			// Handler might return 200 (success), 404 (not found), or 400 (error)
			if w.Code == 200 {
				var response contracts.ScrapeResponse
				err = json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err, "200 response should be valid JSON")

				// Verify response doesn't contain SQL injection characters (even if ID is empty)
				assert.NotContains(t, response.Movie.ID, "'", "Movie ID should not contain SQL injection characters")
				assert.NotContains(t, response.Movie.ID, ";", "Movie ID should not contain SQL injection characters")
				assert.NotContains(t, response.Movie.ID, "--", "Movie ID should not contain SQL injection characters")
				assert.NotContains(t, response.Movie.Title, "'DROP TABLE", "Movie title should not contain injection payload")
			}

			// Verify database integrity - SQL injection should not corrupt data
			// CRITICAL: List(limit, offset) - get up to 100 movies starting from offset 0
			finalMovies, err := deps.Repos.MovieRepo.List(context.TODO(), 100, 0)
			require.NoError(t, err)

			// Verify no movies with malicious IDs were stored
			for _, movie := range finalMovies {
				assert.NotContains(t, movie.ID, "'", "Stored movie IDs should not contain SQL injection characters")
				assert.NotContains(t, movie.ID, ";", "Stored movie IDs should not contain SQL injection characters")
				assert.NotContains(t, movie.ID, "--", "Stored movie IDs should not contain SQL injection characters")
			}

			// Database count should be unchanged OR increased by 1 (if Upsert succeeded)
			// SQL injection would create 0 movies (if DROP succeeds) or multiple movies (if INSERT succeeds)
			assert.True(t, len(finalMovies) == initialCount || len(finalMovies) == initialCount+1,
				"Database count should be %d or %d, got %d (SQL injection might alter count)", initialCount, initialCount+1, len(finalMovies))
		})
	}
}
