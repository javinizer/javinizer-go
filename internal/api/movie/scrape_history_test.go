package movie

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestScrapeHistoryAuditMatrix(t *testing.T) {
	type testCase struct {
		name              string
		requestBody       interface{}
		setupScraper      func(*scraperutil.ScraperRegistry)
		expectedStatus    int
		wantHistoryRows   int
		wantHistoryStatus models.HistoryStatus
		wantHistoryOp     models.HistoryOperation
	}

	cases := []testCase{
		{
			name:        "successful scrape writes success history row",
			requestBody: contracts.ScrapeRequest{ID: "HIST-001"},
			setupScraper: func(registry *scraperutil.ScraperRegistry) {
				registry.RegisterInstance(&mockScraperWithResults{
					name:    "r18dev",
					enabled: true,
					result: &models.ScraperResult{
						Source: "r18dev",
						ID:     "HIST-001",
						Title:  "History Test Movie",
					},
				})
			},
			expectedStatus:    200,
			wantHistoryRows:   1,
			wantHistoryStatus: models.HistoryStatusSuccess,
			wantHistoryOp:     models.HistoryOpScrape,
		},
		{
			name:              "scraper failure writes failed history row",
			requestBody:       contracts.ScrapeRequest{ID: "HIST-FAIL"},
			setupScraper:      func(_ *scraperutil.ScraperRegistry) {},
			expectedStatus:    404,
			wantHistoryRows:   1,
			wantHistoryStatus: models.HistoryStatusFailed,
			wantHistoryOp:     models.HistoryOpScrape,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev"},
				},
			}
			deps := createTestDeps(t, cfg, "")
			movieDeps := NewMovieDeps(deps.Repos.MovieRepo,
				WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow),
				WithHistoryRepo(deps.Repos.HistoryRepo),
			)
			tc.setupScraper(deps.CoreDeps.GetRegistry())

			router := gin.New()
			router.POST("/scrape", scrapeMovie(movieDeps))

			body, _ := json.Marshal(tc.requestBody)
			req := httptest.NewRequest("POST", "/scrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)

			records, err := deps.Repos.HistoryRepo.FindByOperation(context.Background(), models.HistoryOpScrape, 100)
			require.NoError(t, err)
			var testRecords []models.History
			reqID := tc.requestBody.(contracts.ScrapeRequest).ID
			for _, r := range records {
				if r.OriginalPath == reqID {
					testRecords = append(testRecords, r)
				}
			}
			assert.Len(t, testRecords, tc.wantHistoryRows)
			if tc.wantHistoryRows == 1 {
				assert.Equal(t, tc.wantHistoryStatus, testRecords[0].Status)
				assert.Equal(t, tc.wantHistoryOp, testRecords[0].Operation)
			}
		})
	}
}
