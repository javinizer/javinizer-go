package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScrapeMovie(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		setupData      func(*testing.T, *database.MovieRepository)
		setupScraper   func(*models.ScraperRegistry)
		expectedStatus int
		validateFn     func(*testing.T, *ScrapeResponse)
	}{
		{
			name: "successful scrape - not cached",
			requestBody: ScrapeRequest{
				ID: "IPX-535",
			},
			setupData: func(_ *testing.T, repo *database.MovieRepository) {
				// Empty - not cached
			},
			setupScraper: func(registry *models.ScraperRegistry) {
				scraper := &mockScraperWithResults{
					name:    "r18dev",
					enabled: true,
					result: &models.ScraperResult{
						Source: "r18dev",
						ID:     "IPX-535",
						Title:  "Test Movie",
					},
				}
				registry.Register(scraper)
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *ScrapeResponse) {
				assert.False(t, resp.Cached)
				assert.NotNil(t, resp.Movie)
				assert.Equal(t, "IPX-535", resp.Movie.ID)
			},
		},
		{
			name: "successful scrape - from cache",
			requestBody: ScrapeRequest{
				ID: "IPX-535",
			},
			setupData: func(t *testing.T, repo *database.MovieRepository) {
				require.NoError(t, repo.Upsert(&models.Movie{
					ID:    "IPX-535",
					Title: "Cached Movie",
				}))
			},
			setupScraper:   func(registry *models.ScraperRegistry) {},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *ScrapeResponse) {
				assert.True(t, resp.Cached)
				assert.Equal(t, "Cached Movie", resp.Movie.Title)
			},
		},
		{
			name: "invalid request - missing ID",
			requestBody: map[string]string{
				"invalid": "data",
			},
			setupData:      func(_ *testing.T, repo *database.MovieRepository) {},
			setupScraper:   func(registry *models.ScraperRegistry) {},
			expectedStatus: 400,
		},
		{
			name: "movie not found - all scrapers fail",
			requestBody: ScrapeRequest{
				ID: "INVALID-123",
			},
			setupData: func(_ *testing.T, repo *database.MovieRepository) {},
			setupScraper: func(registry *models.ScraperRegistry) {
				registry.Register(&mockScraperWithResults{
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
			tt.setupData(t, deps.MovieRepo)
			tt.setupScraper(deps.Registry)

			router := gin.New()
			router.POST("/scrape", scrapeMovie(deps))

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
				var response ScrapeResponse
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
		setupData      func(*testing.T, *database.MovieRepository)
		expectedStatus int
		validateFn     func(*testing.T, *MovieResponse)
	}{
		{
			name:    "get existing movie",
			movieID: "IPX-535",
			setupData: func(t *testing.T, repo *database.MovieRepository) {
				require.NoError(t, repo.Upsert(&models.Movie{
					ID:    "IPX-535",
					Title: "Test Movie",
				}))
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *MovieResponse) {
				assert.NotNil(t, resp.Movie)
				assert.Equal(t, "IPX-535", resp.Movie.ID)
			},
		},
		{
			name:           "movie not found",
			movieID:        "NONEXISTENT-123",
			setupData:      func(_ *testing.T, repo *database.MovieRepository) {},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")
			tt.setupData(t, deps.MovieRepo)

			router := gin.New()
			router.GET("/movie/:id", getMovie(deps))

			req := httptest.NewRequest("GET", "/movie/"+tt.movieID, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response MovieResponse
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
		setupData      func(*testing.T, *database.MovieRepository)
		expectedStatus int
		validateFn     func(*testing.T, *MoviesResponse)
	}{
		{
			name: "list multiple movies",
			setupData: func(t *testing.T, repo *database.MovieRepository) {
				require.NoError(t, repo.Upsert(&models.Movie{ContentID: "ipx535", ID: "IPX-535", Title: "Movie 1"}))
				require.NoError(t, repo.Upsert(&models.Movie{ContentID: "abc123", ID: "ABC-123", Title: "Movie 2"}))
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *MoviesResponse) {
				assert.Len(t, resp.Movies, 2)
				assert.Equal(t, 2, resp.Count)
			},
		},
		{
			name:           "list empty database",
			setupData:      func(_ *testing.T, repo *database.MovieRepository) {},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *MoviesResponse) {
				assert.Empty(t, resp.Movies)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")
			tt.setupData(t, deps.MovieRepo)

			router := gin.New()
			router.GET("/movies", listMovies(deps))

			req := httptest.NewRequest("GET", "/movies", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response MoviesResponse
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
			deps.Registry.Register(stubScraper)

			router := gin.New()
			router.POST("/scrape", scrapeMovie(deps))

			// Get initial movie count (should be 0)
			// CRITICAL: List(limit, offset) - not List(offset, limit)!
			initialMovies, err := deps.MovieRepo.List(100, 0)
			require.NoError(t, err)
			initialCount := len(initialMovies)

			reqBody := ScrapeRequest{ID: maliciousID}
			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/scrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Tighten status code assertion: expect 200 (success), 400 (bad request), or 404 (not found)
			// Don't accept 3xx, 5xx, or other unexpected codes
			assert.True(t, w.Code == 200 || w.Code == 400 || w.Code == 404,
				"Expected 200/400/404, got %d (server error or unexpected redirect indicates potential injection vulnerability)", w.Code)

			// CRITICAL: Verify response doesn't contain malicious injection payload
			// Handler might return 200 (success), 404 (not found), or 400 (error)
			if w.Code == 200 {
				var response ScrapeResponse
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
			finalMovies, err := deps.MovieRepo.List(100, 0)
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

func TestReorderWithPriority(t *testing.T) {
	tests := []struct {
		name     string
		scrapers []string
		priority string
		expected []string
	}{
		{
			name:     "priority at start",
			scrapers: []string{"r18dev", "dmm", "javlibrary"},
			priority: "r18dev",
			expected: []string{"r18dev", "dmm", "javlibrary"},
		},
		{
			name:     "priority in middle",
			scrapers: []string{"r18dev", "dmm", "javlibrary"},
			priority: "dmm",
			expected: []string{"dmm", "r18dev", "javlibrary"},
		},
		{
			name:     "priority at end",
			scrapers: []string{"r18dev", "dmm", "javlibrary"},
			priority: "javlibrary",
			expected: []string{"javlibrary", "r18dev", "dmm"},
		},
		{
			name:     "priority not in list",
			scrapers: []string{"r18dev", "dmm"},
			priority: "javlibrary",
			expected: []string{"javlibrary", "r18dev", "dmm"},
		},
		{
			name:     "empty scrapers list",
			scrapers: []string{},
			priority: "r18dev",
			expected: []string{"r18dev"},
		},
		{
			name:     "single scraper - same as priority",
			scrapers: []string{"r18dev"},
			priority: "r18dev",
			expected: []string{"r18dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reorderWithPriority(tt.scrapers, tt.priority)
			assert.Equal(t, tt.expected, result)
		})
	}
}
