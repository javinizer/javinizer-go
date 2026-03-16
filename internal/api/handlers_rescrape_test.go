package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRescrapeMovie(t *testing.T) {
	tests := []struct {
		name           string
		movieID        string
		requestBody    interface{}
		setupData      func(*testing.T, *ServerDependencies)
		setupScraper   func(*models.ScraperRegistry)
		expectedStatus int
		validateFn     func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:    "successful rescrape with custom scrapers",
			movieID: "IPX-123",
			requestBody: RescrapeRequest{
				SelectedScrapers: []string{"r18dev"},
				Force:            true,
			},
			setupData: func(t *testing.T, deps *ServerDependencies) {
				// Pre-populate with cached movie
				require.NoError(t, deps.MovieRepo.Upsert(&models.Movie{
					ID:    "IPX-123",
					Title: "Old Title",
				}))
			},
			setupScraper: func(registry *models.ScraperRegistry) {
				registry.Register(&mockScraperWithResults{
					name:    "r18dev",
					enabled: true,
					result: &models.ScraperResult{
						Source: "r18dev",
						ID:     "IPX-123",
						Title:  "New Rescraped Title",
					},
				})
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp MovieResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, "New Rescraped Title", resp.Movie.Title)
				assert.Equal(t, "IPX-123", resp.Movie.ID)
			},
		},
		{
			name:    "rescrape without force - cache cleared by custom scrapers",
			movieID: "IPX-456",
			requestBody: RescrapeRequest{
				SelectedScrapers: []string{"dmm"},
				Force:            false,
			},
			setupData: func(t *testing.T, deps *ServerDependencies) {
				require.NoError(t, deps.MovieRepo.Upsert(&models.Movie{
					ID:    "IPX-456",
					Title: "Cached Title",
				}))
			},
			setupScraper: func(registry *models.ScraperRegistry) {
				registry.Register(&mockScraperWithResults{
					name:    "dmm",
					enabled: true,
					result: &models.ScraperResult{
						Source: "dmm",
						ID:     "IPX-456",
						Title:  "Fresh Scraped Title",
					},
				})
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp MovieResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, "Fresh Scraped Title", resp.Movie.Title)
			},
		},
		{
			name:    "rescrape with invalid request body",
			movieID: "IPX-789",
			requestBody: map[string]string{
				"invalid": "field",
			},
			setupData:      func(_ *testing.T, deps *ServerDependencies) {},
			setupScraper:   func(registry *models.ScraperRegistry) {},
			expectedStatus: 400,
		},
		{
			name:    "rescrape with empty selected scrapers",
			movieID: "IPX-999",
			requestBody: RescrapeRequest{
				SelectedScrapers: []string{},
				Force:            false,
			},
			setupData:      func(_ *testing.T, deps *ServerDependencies) {},
			setupScraper:   func(registry *models.ScraperRegistry) {},
			expectedStatus: 400,
			validateFn: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Contains(t, resp.Error, "selected_scrapers cannot be empty")
			},
		},
		{
			name:    "rescrape with all scrapers failing",
			movieID: "NOTFOUND-123",
			requestBody: RescrapeRequest{
				SelectedScrapers: []string{"r18dev", "dmm"},
				Force:            false,
			},
			setupData: func(_ *testing.T, deps *ServerDependencies) {},
			setupScraper: func(registry *models.ScraperRegistry) {
				registry.Register(&mockScraperWithResults{
					name:    "r18dev",
					enabled: true,
					err:     assert.AnError,
				})
				registry.Register(&mockScraperWithResults{
					name:    "dmm",
					enabled: true,
					err:     assert.AnError,
				})
			},
			expectedStatus: 404,
			validateFn: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Contains(t, resp.Error, "No results from selected scrapers")
				assert.NotEmpty(t, resp.Errors)
			},
		},
		{
			name:    "rescrape with partial scraper failures",
			movieID: "IPX-111",
			requestBody: RescrapeRequest{
				SelectedScrapers: []string{"r18dev", "dmm"},
				Force:            false,
			},
			setupData: func(_ *testing.T, deps *ServerDependencies) {},
			setupScraper: func(registry *models.ScraperRegistry) {
				// First scraper fails
				registry.Register(&mockScraperWithResults{
					name:    "r18dev",
					enabled: true,
					err:     assert.AnError,
				})
				// Second scraper succeeds
				registry.Register(&mockScraperWithResults{
					name:    "dmm",
					enabled: true,
					result: &models.ScraperResult{
						Source: "dmm",
						ID:     "IPX-111",
						Title:  "Partial Success Title",
					},
				})
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp MovieResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, "Partial Success Title", resp.Movie.Title)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev", "dmm"},
				},
			}

			deps := createTestDeps(t, cfg, "")
			tt.setupData(t, deps)
			tt.setupScraper(deps.Registry)

			router := gin.New()
			router.POST("/movies/:id/rescrape", rescrapeMovie(deps))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/movies/"+tt.movieID+"/rescrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				tt.validateFn(t, w)
			}
		})
	}
}
