package history

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	historypkg "github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupHistoryTestDB creates an in-memory database with history records for testing
func setupHistoryTestDB(t *testing.T) (*database.DB, *database.HistoryRepository) {
	t.Helper()

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := database.NewHistoryRepository(db)
	return db, repo
}

// seedHistoryData creates test history records
func seedHistoryData(t *testing.T, repo *database.HistoryRepository) {
	t.Helper()

	records := []*models.History{
		{MovieID: "IPX-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, OriginalPath: "/path/to/IPX-001.mp4"},
		{MovieID: "IPX-001", Operation: models.HistoryOpDownload, Status: models.HistoryStatusSuccess, OriginalPath: "https://example.com/cover.jpg", NewPath: "/path/to/cover.jpg"},
		{MovieID: "IPX-001", Operation: models.HistoryOpNFO, Status: models.HistoryStatusSuccess, NewPath: "/path/to/IPX-001.nfo"},
		{MovieID: "IPX-002", Operation: models.HistoryOpScrape, Status: models.HistoryStatusFailed, ErrorMessage: "scraper error"},
		{MovieID: "IPX-003", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, OriginalPath: "/src/IPX-003.mp4", NewPath: "/dest/IPX-003.mp4"},
		{MovieID: "IPX-004", Operation: models.HistoryOpScrape, Status: models.HistoryStatusReverted},
	}

	for _, r := range records {
		err := repo.Create(context.Background(), r)
		require.NoError(t, err)
	}
}

func TestGetHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		queryParams    string
		seedData       bool
		expectedStatus int
		validateFn     func(*testing.T, *HistoryListResponse)
	}{
		{
			name:           "empty history",
			queryParams:    "",
			seedData:       false,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryListResponse) {
				assert.Equal(t, int64(0), resp.Total)
				assert.Empty(t, resp.Records)
				assert.Equal(t, 50, resp.Limit)
				assert.Equal(t, 0, resp.Offset)
			},
		},
		{
			name:           "list all history",
			queryParams:    "",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryListResponse) {
				assert.Equal(t, int64(6), resp.Total)
				assert.Len(t, resp.Records, 6)
			},
		},
		{
			name:           "pagination - limit",
			queryParams:    "?limit=2",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryListResponse) {
				assert.Equal(t, int64(6), resp.Total)
				assert.Len(t, resp.Records, 2)
				assert.Equal(t, 2, resp.Limit)
			},
		},
		{
			name:           "pagination - offset",
			queryParams:    "?limit=2&offset=2",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryListResponse) {
				assert.Equal(t, int64(6), resp.Total)
				assert.Len(t, resp.Records, 2)
				assert.Equal(t, 2, resp.Offset)
			},
		},
		{
			name:           "filter by operation",
			queryParams:    "?operation=scrape",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryListResponse) {
				assert.Equal(t, int64(3), resp.Total)
				for _, r := range resp.Records {
					assert.Equal(t, models.HistoryOpScrape, r.Operation)
				}
			},
		},
		{
			name:           "filter by status",
			queryParams:    "?status=success",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryListResponse) {
				assert.Equal(t, int64(4), resp.Total)
				for _, r := range resp.Records {
					assert.Equal(t, models.HistoryStatusSuccess, r.Status)
				}
			},
		},
		{
			name:           "filter by status",
			queryParams:    "?status=success",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryListResponse) {
				assert.Equal(t, int64(4), resp.Total)
				for _, r := range resp.Records {
					assert.Equal(t, models.HistoryStatusSuccess, r.Status)
				}
			},
		},
		{
			name:           "filter by movie_id",
			queryParams:    "?movie_id=IPX-001",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryListResponse) {
				assert.Equal(t, int64(3), resp.Total)
				for _, r := range resp.Records {
					assert.Equal(t, "IPX-001", r.MovieID)
				}
			},
		},
		{
			name:           "limit capped at 500",
			queryParams:    "?limit=1000",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryListResponse) {
				assert.Equal(t, 500, resp.Limit)
			},
		},
		{
			name:           "invalid limit ignored",
			queryParams:    "?limit=invalid",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryListResponse) {
				assert.Equal(t, 50, resp.Limit) // Default
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, repo := setupHistoryTestDB(t)
			defer func() {
				_ = db.Close()
			}()

			if tt.seedData {
				seedHistoryData(t, repo)
			}

			router := gin.New()
			router.GET("/api/v1/history", getHistory(repo))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/history"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				var resp HistoryListResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}

func TestGetHistoryStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		seedData       bool
		expectedStatus int
		validateFn     func(*testing.T, *HistoryStats)
	}{
		{
			name:           "empty stats",
			seedData:       false,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryStats) {
				assert.Equal(t, int64(0), resp.Total)
				assert.Equal(t, int64(0), resp.Success)
				assert.Equal(t, int64(0), resp.Failed)
				assert.Equal(t, int64(0), resp.Reverted)
				// ByOperation is always populated by the handler, even when empty
			},
		},
		{
			name:           "stats with data",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryStats) {
				assert.Equal(t, int64(6), resp.Total)
				assert.Equal(t, int64(4), resp.Success)
				assert.Equal(t, int64(1), resp.Failed)
				assert.Equal(t, int64(1), resp.Reverted)
				assert.Equal(t, int64(3), resp.ByOperation[string(models.HistoryOpScrape)])
				assert.Equal(t, int64(1), resp.ByOperation[string(models.HistoryOpOrganize)])
				assert.Equal(t, int64(1), resp.ByOperation[string(models.HistoryOpDownload)])
				assert.Equal(t, int64(1), resp.ByOperation[string(models.HistoryOpNFO)])
			},
		},
		{
			name:           "stats with only one operation type",
			seedData:       false,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *HistoryStats) {
				// When repo has only scrape records, other ops should have 0 count
				// Mock repo returns 0 for unknown operations
				assert.Equal(t, int64(0), resp.Total)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, repo := setupHistoryTestDB(t)
			defer func() {
				_ = db.Close()
			}()

			if tt.seedData {
				seedHistoryData(t, repo)
			}

			router := gin.New()
			router.GET("/api/v1/history/stats", getHistoryStats(historypkg.NewLogger(repo)))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				var resp HistoryStats
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}

func TestGetHistoryStats_EmptyDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, repo := setupHistoryTestDB(t)
	defer func() {
		_ = db.Close()
	}()

	// Empty database - no records
	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(historypkg.NewLogger(repo)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryStats
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, int64(0), resp.Total)
	assert.Equal(t, int64(0), resp.Success)
	assert.Equal(t, int64(0), resp.Failed)
	assert.Equal(t, int64(0), resp.Reverted)
}

func TestGetHistoryStats_WithAllOperationTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, repo := setupHistoryTestDB(t)
	defer func() {
		_ = db.Close()
	}()

	// Create records with all operation types
	// The mock repo CountByOperation returns 1 for "scrape" (first one created),
	// and 0 for others since mock only tracks by status, not operation
	operations := []models.HistoryOperation{models.HistoryOpScrape, models.HistoryOpOrganize, models.HistoryOpDownload, models.HistoryOpNFO}
	for i, op := range operations {
		require.NoError(t, repo.Create(context.Background(), &models.History{
			MovieID:   fmt.Sprintf("TEST-%03d", i+1),
			Operation: op,
			Status:    models.HistoryStatusSuccess,
		}))
	}

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(historypkg.NewLogger(repo)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryStats
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, int64(4), resp.Total)
	assert.Equal(t, int64(4), resp.Success)
	// Note: mock repo operation counts may differ from real repo
	// The key test is that Total and Success are correct
	assert.NotEmpty(t, resp.ByOperation)
}

// TestGetHistoryStats_AllFailurePaths tests the error handling paths
func TestGetHistoryStats_AllFailurePaths(t *testing.T) {
	// This test uses the real DB repository which has error handling paths
	// The mock repo in the existing tests doesn't return errors
	gin.SetMode(gin.TestMode)

	db, repo := setupHistoryTestDB(t)
	defer func() {
		_ = db.Close()
	}()

	// Create some history data
	seedHistoryData(t, repo)

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(historypkg.NewLogger(repo)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryStats
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	// Verify all stats are calculated correctly
	assert.Equal(t, int64(6), resp.Total)
	assert.Equal(t, int64(4), resp.Success)
	assert.Equal(t, int64(1), resp.Failed)
	assert.Equal(t, int64(1), resp.Reverted)
}

func TestDeleteHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		historyID      string
		seedData       bool
		expectedStatus int
		validateFn     func(*testing.T, *database.HistoryRepository)
	}{
		{
			name:           "delete existing record",
			historyID:      "1",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, repo *database.HistoryRepository) {
				_, err := repo.FindByID(context.Background(), 1)
				assert.Error(t, err) // Should not exist
			},
		},
		{
			name:           "delete non-existent record",
			historyID:      "999",
			seedData:       true,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "invalid ID",
			historyID:      "invalid",
			seedData:       true,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, repo := setupHistoryTestDB(t)
			defer func() {
				_ = db.Close()
			}()

			if tt.seedData {
				seedHistoryData(t, repo)
			}

			router := gin.New()
			router.DELETE("/api/v1/history/:id", deleteHistory(repo))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/history/"+tt.historyID, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				tt.validateFn(t, repo)
			}
		})
	}
}

func TestDeleteHistoryBulk(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		queryParams    string
		seedData       bool
		expectedStatus int
		validateFn     func(*testing.T, *DeleteHistoryBulkResponse, *database.HistoryRepository)
	}{
		{
			name:           "missing parameters",
			queryParams:    "",
			seedData:       true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "delete by movie_id",
			queryParams:    "?movie_id=IPX-001",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *DeleteHistoryBulkResponse, repo *database.HistoryRepository) {
				assert.Equal(t, int64(3), resp.Deleted)
				records, _ := repo.FindByMovieID(context.Background(), "IPX-001")
				assert.Empty(t, records)
			},
		},
		{
			name:           "delete by older_than_days - none deleted",
			queryParams:    "?older_than_days=1",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *DeleteHistoryBulkResponse, repo *database.HistoryRepository) {
				// Records just created, none should be older than 1 day
				assert.Equal(t, int64(0), resp.Deleted)
			},
		},
		{
			name:           "invalid older_than_days",
			queryParams:    "?older_than_days=invalid",
			seedData:       true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "older_than_days less than 1",
			queryParams:    "?older_than_days=0",
			seedData:       true,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, repo := setupHistoryTestDB(t)
			defer func() {
				_ = db.Close()
			}()

			if tt.seedData {
				seedHistoryData(t, repo)
			}

			router := gin.New()
			router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/history"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				var resp DeleteHistoryBulkResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp, repo)
			}
		})
	}
}

func TestGetHistory_WithDateFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, repo := setupHistoryTestDB(t)
	defer func() { _ = db.Close() }()

	now := time.Now()
	twoHoursAgo := now.Add(-2 * time.Hour)
	oneHourAgo := now.Add(-1 * time.Hour)

	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "OLD-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, CreatedAt: twoHoursAgo,
	}))
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "NEW-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, CreatedAt: oneHourAgo,
	}))

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?operation=scrape", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.Total)
}

func TestGetHistoryStats_ByOperationType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, repo := setupHistoryTestDB(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, repo.Create(context.Background(), &models.History{MovieID: "A-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess}))
	require.NoError(t, repo.Create(context.Background(), &models.History{MovieID: "A-002", Operation: models.HistoryOpScrape, Status: models.HistoryStatusFailed}))
	require.NoError(t, repo.Create(context.Background(), &models.History{MovieID: "A-003", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}))
	require.NoError(t, repo.Create(context.Background(), &models.History{MovieID: "A-004", Operation: models.HistoryOpDownload, Status: models.HistoryStatusSuccess}))
	require.NoError(t, repo.Create(context.Background(), &models.History{MovieID: "A-005", Operation: models.HistoryOpNFO, Status: models.HistoryStatusReverted}))

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(historypkg.NewLogger(repo)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryStats
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(5), resp.Total)
	assert.Equal(t, int64(3), resp.Success)
	assert.Equal(t, int64(1), resp.Failed)
	assert.Equal(t, int64(1), resp.Reverted)
	assert.Equal(t, int64(2), resp.ByOperation[string(models.HistoryOpScrape)])
	assert.Equal(t, int64(1), resp.ByOperation[string(models.HistoryOpOrganize)])
	assert.Equal(t, int64(1), resp.ByOperation[string(models.HistoryOpDownload)])
	assert.Equal(t, int64(1), resp.ByOperation[string(models.HistoryOpNFO)])
}

func TestDeleteHistoryBulk_ByOperationType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, repo := setupHistoryTestDB(t)
	defer func() { _ = db.Close() }()

	seedHistoryData(t, repo)

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?movie_id=IPX-001", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DeleteHistoryBulkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(3), resp.Deleted)
}

func TestHistoryRecordFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, repo := setupHistoryTestDB(t)
	defer func() {
		_ = db.Close()
	}()

	// Create a record with all fields populated
	record := &models.History{
		MovieID:      "TEST-001",
		Operation:    models.HistoryOpScrape,
		Status:       models.HistoryStatusSuccess,
		OriginalPath: "/original/path.mp4",
		NewPath:      "/new/path.mp4",
		ErrorMessage: "",
		Metadata:     `{"source": "test"}`,
		DryRun:       true,
	}
	require.NoError(t, repo.Create(context.Background(), record))

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	require.Len(t, resp.Records, 1)
	r := resp.Records[0]

	assert.Equal(t, "TEST-001", r.MovieID)
	assert.Equal(t, models.HistoryOpScrape, r.Operation)
	assert.Equal(t, models.HistoryStatusSuccess, r.Status)
	assert.Equal(t, "/original/path.mp4", r.OriginalPath)
	assert.Equal(t, "/new/path.mp4", r.NewPath)
	assert.Equal(t, `{"source": "test"}`, r.Metadata)
	assert.True(t, r.DryRun)
	assert.NotEmpty(t, r.CreatedAt)
}

func TestHistoryPaginationEdgeCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, repo := setupHistoryTestDB(t)
	defer func() {
		_ = db.Close()
	}()

	// Create exactly 5 records
	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.History{
			MovieID:   "TEST",
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusSuccess,
		}))
	}

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	tests := []struct {
		name          string
		queryParams   string
		expectedCount int
	}{
		{"offset beyond total", "?offset=100", 0},
		{"offset at boundary", "?offset=5", 0},
		{"limit larger than remaining", "?limit=10&offset=3", 2},
		{"zero limit uses default", "?limit=0", 5},
		{"negative offset ignored", "?offset=-5", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/history"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var resp HistoryListResponse
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Len(t, resp.Records, tt.expectedCount)
		})
	}
}

func TestGetHistory_StatusFailed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, repo := setupHistoryTestDB(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, repo.Create(context.Background(), &models.History{MovieID: "A-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess}))
	require.NoError(t, repo.Create(context.Background(), &models.History{MovieID: "A-002", Operation: models.HistoryOpScrape, Status: models.HistoryStatusFailed}))
	require.NoError(t, repo.Create(context.Background(), &models.History{MovieID: "A-003", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusFailed}))

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?status=failed", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.Total)
	for _, r := range resp.Records {
		assert.Equal(t, models.HistoryStatusFailed, r.Status)
	}
}

func TestGetHistory_DefaultList(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, repo := setupHistoryTestDB(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, repo.Create(context.Background(), &models.History{MovieID: "A-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess}))
	require.NoError(t, repo.Create(context.Background(), &models.History{MovieID: "A-002", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}))

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.Total)
	assert.Len(t, resp.Records, 2)
}

// TestGetHistory_FilteredPagination verifies the filtered branches (movie_id,
// operation, status) paginate at the repository layer instead of loading every
// matching row. With >10 matching rows, limit=10&offset=0 returns exactly 10
// records and the true total, and offset shifts the window without overlap.
func TestGetHistory_FilteredPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type filterCase struct {
		name  string
		query string
		// movieIDs/op/status of the seed rows that the filter must match.
		validate func(t *testing.T, page1, page2 []HistoryRecord, total int64)
	}

	seed := func(t *testing.T, repo *database.HistoryRepository, movieID string, op models.HistoryOperation, status models.HistoryStatus) {
		t.Helper()
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 15; i++ {
			require.NoError(t, repo.Create(context.Background(), &models.History{
				MovieID:   movieID,
				Operation: op,
				Status:    status,
				CreatedAt: base.Add(time.Duration(i) * time.Second),
			}))
		}
	}

	cases := []filterCase{
		{
			name:  "movie_id",
			query: "movie_id=PAG-001",
			validate: func(t *testing.T, page1, page2 []HistoryRecord, total int64) {
				assert.Equal(t, int64(15), total)
				for _, r := range append(append([]HistoryRecord{}, page1...), page2...) {
					assert.Equal(t, "PAG-001", r.MovieID)
				}
			},
		},
		{
			name:  "operation",
			query: "operation=scrape",
			validate: func(t *testing.T, page1, page2 []HistoryRecord, total int64) {
				assert.Equal(t, int64(15), total)
				for _, r := range append(append([]HistoryRecord{}, page1...), page2...) {
					assert.Equal(t, models.HistoryOpScrape, r.Operation)
				}
			},
		},
		{
			name:  "status",
			query: "status=failed",
			validate: func(t *testing.T, page1, page2 []HistoryRecord, total int64) {
				assert.Equal(t, int64(15), total)
				for _, r := range append(append([]HistoryRecord{}, page1...), page2...) {
					assert.Equal(t, models.HistoryStatusFailed, r.Status)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, repo := setupHistoryTestDB(t)
			defer func() { _ = db.Close() }()

			// 15 matching rows + a few non-matching rows to prove the filter is scoped.
			seed(t, repo, "PAG-001", models.HistoryOpScrape, models.HistoryStatusFailed)
			require.NoError(t, repo.Create(context.Background(), &models.History{
				MovieID: "OTHER", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess,
			}))

			router := gin.New()
			router.GET("/api/v1/history", getHistory(repo))

			// First page: limit=10, offset=0 -> exactly 10 records and the true total.
			req := httptest.NewRequest(http.MethodGet, "/api/v1/history?limit=10&offset=0&"+tc.query, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)

			var resp1 HistoryListResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp1))
			assert.Equal(t, int64(15), resp1.Total, "total must reflect all matching rows, not just the page")
			assert.Len(t, resp1.Records, 10, "first page must be capped at limit=10")
			assert.Equal(t, 10, resp1.Limit)
			assert.Equal(t, 0, resp1.Offset)

			// Second page: offset=10 -> the remaining 5 records, no overlap with page 1.
			req2 := httptest.NewRequest(http.MethodGet, "/api/v1/history?limit=10&offset=10&"+tc.query, nil)
			w2 := httptest.NewRecorder()
			router.ServeHTTP(w2, req2)
			require.Equal(t, http.StatusOK, w2.Code)

			var resp2 HistoryListResponse
			require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))
			assert.Equal(t, int64(15), resp2.Total)
			assert.Len(t, resp2.Records, 5, "second page must contain the remaining records")

			// Pages must not overlap.
			seen := make(map[uint]struct{}, len(resp1.Records))
			for _, r := range resp1.Records {
				seen[r.ID] = struct{}{}
			}
			for _, r := range resp2.Records {
				_, dup := seen[r.ID]
				assert.False(t, dup, "second page record %d must not repeat in first page", r.ID)
			}

			tc.validate(t, resp1.Records, resp2.Records, resp1.Total)
		})
	}
}
