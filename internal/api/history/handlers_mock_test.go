package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	historypkg "github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// mockAnyTime is a testify-mock matcher for any time.Time argument.
// Used with repo.On() style expectations since EXPECT() does not support matchers.
var mockAnyTime = mock.AnythingOfType("time.Time")

// newMockRepo creates a MockHistoryRepositoryInterface for use in handler tests.
func newMockRepo(t *testing.T) *mocks.MockHistoryRepositoryInterface {
	return mocks.NewMockHistoryRepositoryInterface(t)
}

// ---------------------------------------------------------------------------
// getHistory error paths
// ---------------------------------------------------------------------------

func TestGetHistory_FindByMovieIDError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().FindByMovieID(context.Background(), "ABC-123").Return(nil, errors.New("db error"))

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?movie_id=ABC-123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to retrieve history", resp.Error)
}

func TestGetHistory_FindByOperationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().FindByOperation(context.Background(), models.HistoryOpScrape, 0).Return(nil, errors.New("db error"))

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?operation=scrape", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to retrieve history", resp.Error)
}

func TestGetHistory_FindByStatusError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().FindByStatus(context.Background(), models.HistoryStatusFailed, 0).Return(nil, errors.New("db error"))

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?status=failed", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to retrieve history", resp.Error)
}

func TestGetHistory_CountError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().Count(context.Background()).Return(int64(0), errors.New("db error"))

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to count history", resp.Error)
}

func TestGetHistory_ListError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().Count(context.Background()).Return(int64(10), nil)
	repo.EXPECT().List(context.Background(), 50, 0).Return(nil, errors.New("db error"))

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to retrieve history", resp.Error)
}

func TestGetHistory_MovieIDWithPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := []models.History{
		{ID: 1, MovieID: "ABC-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, CreatedAt: ts},
		{ID: 2, MovieID: "ABC-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, CreatedAt: ts},
		{ID: 3, MovieID: "ABC-001", Operation: models.HistoryOpDownload, Status: models.HistoryStatusSuccess, CreatedAt: ts},
	}
	repo.EXPECT().FindByMovieID(context.Background(), "ABC-001").Return(records, nil)

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	// Request with offset=1, limit=1 — should return only the second record
	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?movie_id=ABC-001&limit=1&offset=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(3), resp.Total)
	assert.Len(t, resp.Records, 1)
	assert.Equal(t, uint(2), resp.Records[0].ID)
}

func TestGetHistory_OperationFilterSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := []models.History{
		{ID: 1, MovieID: "ABC-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, CreatedAt: ts},
	}
	repo.EXPECT().FindByOperation(context.Background(), models.HistoryOpOrganize, 0).Return(records, nil)

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?operation=organize", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(1), resp.Total)
	assert.Len(t, resp.Records, 1)
}

func TestGetHistory_StatusFilterReverted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := []models.History{
		{ID: 5, MovieID: "ABC-005", Operation: models.HistoryOpScrape, Status: models.HistoryStatusReverted, CreatedAt: ts},
	}
	repo.EXPECT().FindByStatus(context.Background(), models.HistoryStatusReverted, 0).Return(records, nil)

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?status=reverted", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(1), resp.Total)
	assert.Len(t, resp.Records, 1)
	assert.Equal(t, models.HistoryStatusReverted, resp.Records[0].Status)
}

func TestGetHistory_DefaultListSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := []models.History{
		{ID: 1, MovieID: "ABC-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, CreatedAt: ts},
		{ID: 2, MovieID: "ABC-002", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusFailed, CreatedAt: ts},
	}
	repo.EXPECT().Count(context.Background()).Return(int64(2), nil)
	repo.EXPECT().List(context.Background(), 50, 0).Return(records, nil)

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

// movie_id takes precedence over operation/status when both are provided
func TestGetHistory_MovieIDTakesPrecedenceOverOperation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().FindByMovieID(context.Background(), "ABC-001").Return([]models.History{}, nil)
	// FindByOperation should NOT be called

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?movie_id=ABC-001&operation=scrape", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// operation takes precedence over status when both are provided (no movie_id)
func TestGetHistory_OperationTakesPrecedenceOverStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().FindByOperation(context.Background(), models.HistoryOpScrape, 0).Return([]models.History{}, nil)
	// FindByStatus should NOT be called

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?operation=scrape&status=success", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// getHistoryStats error paths
// ---------------------------------------------------------------------------

func TestGetHistoryStats_LoggerGetStatsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	// Logger.GetStats calls repo.Count first — make it fail
	repo.EXPECT().Count(context.Background()).Return(int64(0), errors.New("db error"))

	logger := historypkg.NewLogger(repo)

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(logger))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to get statistics", resp.Error)
}

func TestGetHistoryStats_CountByStatusError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	// Count succeeds, but CountByStatus(success) fails
	repo.EXPECT().Count(context.Background()).Return(int64(10), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusSuccess).Return(int64(0), errors.New("db error"))

	logger := historypkg.NewLogger(repo)

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(logger))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetHistoryStats_CountByOperationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	// Count and CountByStatus succeed, but CountByOperation fails
	repo.EXPECT().Count(context.Background()).Return(int64(5), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusSuccess).Return(int64(3), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusFailed).Return(int64(1), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusReverted).Return(int64(1), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpScrape).Return(int64(0), errors.New("db error"))

	logger := historypkg.NewLogger(repo)

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(logger))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetHistoryStats_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().Count(context.Background()).Return(int64(10), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusSuccess).Return(int64(6), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusFailed).Return(int64(3), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusReverted).Return(int64(1), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpScrape).Return(int64(4), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpOrganize).Return(int64(3), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpDownload).Return(int64(2), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpNFO).Return(int64(1), nil)

	logger := historypkg.NewLogger(repo)

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(logger))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryStats
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(10), resp.Total)
	assert.Equal(t, int64(6), resp.Success)
	assert.Equal(t, int64(3), resp.Failed)
	assert.Equal(t, int64(1), resp.Reverted)
	assert.Equal(t, int64(4), resp.ByOperation[string(models.HistoryOpScrape)])
	assert.Equal(t, int64(3), resp.ByOperation[string(models.HistoryOpOrganize)])
	assert.Equal(t, int64(2), resp.ByOperation[string(models.HistoryOpDownload)])
	assert.Equal(t, int64(1), resp.ByOperation[string(models.HistoryOpNFO)])
}

// ---------------------------------------------------------------------------
// deleteHistory error paths
// ---------------------------------------------------------------------------

func TestDeleteHistory_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	router := gin.New()
	router.DELETE("/api/v1/history/:id", deleteHistory(repo))

	// Non-numeric ID
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Invalid history ID", resp.Error)
}

func TestDeleteHistory_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().FindByID(context.Background(), uint(999)).Return(nil, errors.New("not found"))

	router := gin.New()
	router.DELETE("/api/v1/history/:id", deleteHistory(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history/999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "History record not found", resp.Error)
}

func TestDeleteHistory_DeleteError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo.EXPECT().FindByID(context.Background(), uint(1)).Return(&models.History{ID: 1, MovieID: "ABC-001", CreatedAt: ts}, nil)
	repo.EXPECT().Delete(context.Background(), uint(1)).Return(errors.New("db error"))

	router := gin.New()
	router.DELETE("/api/v1/history/:id", deleteHistory(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to delete history record", resp.Error)
}

func TestDeleteHistory_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo.EXPECT().FindByID(context.Background(), uint(1)).Return(&models.History{ID: 1, MovieID: "ABC-001", CreatedAt: ts}, nil)
	repo.EXPECT().Delete(context.Background(), uint(1)).Return(nil)

	router := gin.New()
	router.DELETE("/api/v1/history/:id", deleteHistory(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDeleteHistory_NegativeID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	router := gin.New()
	router.DELETE("/api/v1/history/:id", deleteHistory(repo))

	// Negative number — ParseUint will fail
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history/-1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteHistory_ZeroID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	// ID 0 parses fine but FindByID should return not found
	repo.EXPECT().FindByID(context.Background(), uint(0)).Return(nil, errors.New("not found"))

	router := gin.New()
	router.DELETE("/api/v1/history/:id", deleteHistory(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history/0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ---------------------------------------------------------------------------
// deleteHistoryBulk error paths
// ---------------------------------------------------------------------------

func TestDeleteHistoryBulk_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Must specify either older_than_days or movie_id", resp.Error)
}

func TestDeleteHistoryBulk_FindByMovieIDError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().FindByMovieID(context.Background(), "ABC-001").Return(nil, errors.New("db error"))

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?movie_id=ABC-001", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to delete history", resp.Error)
}

func TestDeleteHistoryBulk_DeleteByMovieIDError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().FindByMovieID(context.Background(), "ABC-001").Return([]models.History{{ID: 1, MovieID: "ABC-001"}}, nil)
	repo.EXPECT().DeleteByMovieID(context.Background(), "ABC-001").Return(errors.New("db error"))

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?movie_id=ABC-001", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to delete history", resp.Error)
}

func TestDeleteHistoryBulk_DeleteByMovieIDSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().FindByMovieID(context.Background(), "ABC-001").Return([]models.History{
		{ID: 1, MovieID: "ABC-001"}, {ID: 2, MovieID: "ABC-001"},
	}, nil)
	repo.EXPECT().DeleteByMovieID(context.Background(), "ABC-001").Return(nil)

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?movie_id=ABC-001", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DeleteHistoryBulkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.Deleted)
}

func TestDeleteHistoryBulk_InvalidOlderThanDays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?older_than_days=abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Invalid older_than_days value", resp.Error)
}

func TestDeleteHistoryBulk_OlderThanDaysZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?older_than_days=0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteHistoryBulk_OlderThanDaysNegative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?older_than_days=-5", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteHistoryBulk_CountBeforeError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.On("Count", mock.Anything).Return(int64(0), errors.New("db error")).Once()

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?older_than_days=30", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to count history", resp.Error)
}

func TestDeleteHistoryBulk_DeleteOlderThanError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.On("Count", mock.Anything).Return(int64(10), nil).Once()
	repo.On("DeleteOlderThan", mock.Anything, mock.Anything).Return(errors.New("db error")).Once()

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?older_than_days=30", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to delete history", resp.Error)
}

func TestDeleteHistoryBulk_CountAfterError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.On("Count", mock.Anything).Return(int64(10), nil).Once()
	repo.On("DeleteOlderThan", mock.Anything, mock.Anything).Return(nil).Once()
	repo.On("Count", mock.Anything).Return(int64(0), errors.New("db error")).Once()

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?older_than_days=30", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Failed to count history", resp.Error)
}

func TestDeleteHistoryBulk_OlderThanDaysSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.On("Count", mock.Anything).Return(int64(10), nil).Once()
	repo.On("DeleteOlderThan", mock.Anything, mock.Anything).Return(nil).Once()
	repo.On("Count", mock.Anything).Return(int64(3), nil).Once()

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?older_than_days=30", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DeleteHistoryBulkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(7), resp.Deleted)
}

func TestDeleteHistoryBulk_OlderThanDaysDeletesNone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.On("Count", mock.Anything).Return(int64(5), nil).Once()
	repo.On("DeleteOlderThan", mock.Anything, mock.Anything).Return(nil).Once()
	repo.On("Count", mock.Anything).Return(int64(5), nil).Once()

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?older_than_days=30", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DeleteHistoryBulkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(0), resp.Deleted)
}

// ---------------------------------------------------------------------------
// Integration-style tests with real DB (confirming end-to-end paths)
// ---------------------------------------------------------------------------

func TestGetHistory_MovieIDEmptyString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	// Empty movie_id should fall through to the operation/status/default path
	repo.EXPECT().Count(context.Background()).Return(int64(0), nil)
	repo.EXPECT().List(context.Background(), 50, 0).Return([]models.History{}, nil)

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?movie_id=", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDeleteHistoryBulk_MovieIDEmptyOlderThanDaysSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	// movie_id is empty, older_than_days is set — should use older_than_days path
	repo.On("Count", mock.Anything).Return(int64(5), nil).Once()
	repo.On("DeleteOlderThan", mock.Anything, mock.Anything).Return(nil).Once()
	repo.On("Count", mock.Anything).Return(int64(2), nil).Once()

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?older_than_days=7&movie_id=", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DeleteHistoryBulkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(3), resp.Deleted)
}

func TestGetHistory_MovieIDFilterWithEmptyResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().FindByMovieID(context.Background(), "NONEXISTENT").Return([]models.History{}, nil)

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?movie_id=NONEXISTENT", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(0), resp.Total)
	assert.Empty(t, resp.Records)
}

func TestGetHistory_AllFiltersEmptyFallsToDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	// All query params present but empty — should use default list path
	repo.EXPECT().Count(context.Background()).Return(int64(0), nil)
	repo.EXPECT().List(context.Background(), 50, 0).Return([]models.History{}, nil)

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?movie_id=&operation=&status=", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(0), resp.Total)
}

func TestGetHistory_MovieIDWithOffsetBeyondResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := []models.History{
		{ID: 1, MovieID: "ABC-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, CreatedAt: ts},
	}
	repo.EXPECT().FindByMovieID(context.Background(), "ABC-001").Return(records, nil)

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	// offset=100 is beyond 1 record
	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?movie_id=ABC-001&offset=100", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(1), resp.Total)
	assert.Empty(t, resp.Records) // offset beyond total
}

// ---------------------------------------------------------------------------
// Verify all fields populated in history record API response
// ---------------------------------------------------------------------------

func TestGetHistory_RecordWithAllFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	ts := time.Date(2026, 6, 8, 14, 30, 0, 0, time.UTC)
	records := []models.History{
		{
			ID:           42,
			MovieID:      "XYZ-999",
			Operation:    models.HistoryOpDownload,
			OriginalPath: "/src/file.mp4",
			NewPath:      "/dst/file.mp4",
			Status:       models.HistoryStatusFailed,
			ErrorMessage: "network timeout",
			Metadata:     `{"key":"val"}`,
			DryRun:       true,
			CreatedAt:    ts,
		},
	}
	repo.EXPECT().FindByMovieID(context.Background(), "XYZ-999").Return(records, nil)

	router := gin.New()
	router.GET("/api/v1/history", getHistory(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history?movie_id=XYZ-999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HistoryListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Records, 1)

	r := resp.Records[0]
	assert.Equal(t, uint(42), r.ID)
	assert.Equal(t, "XYZ-999", r.MovieID)
	assert.Equal(t, models.HistoryOpDownload, r.Operation)
	assert.Equal(t, "/src/file.mp4", r.OriginalPath)
	assert.Equal(t, "/dst/file.mp4", r.NewPath)
	assert.Equal(t, models.HistoryStatusFailed, r.Status)
	assert.Equal(t, "network timeout", r.ErrorMessage)
	assert.Equal(t, `{"key":"val"}`, r.Metadata)
	assert.True(t, r.DryRun)
	assert.Equal(t, ts.Format(time.RFC3339), r.CreatedAt)
}

// ---------------------------------------------------------------------------
// getHistoryStats: partial error in CountByOperation chain
// ---------------------------------------------------------------------------

func TestGetHistoryStats_CountByOperationOrganizeError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().Count(context.Background()).Return(int64(5), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusSuccess).Return(int64(3), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusFailed).Return(int64(1), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusReverted).Return(int64(1), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpScrape).Return(int64(2), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpOrganize).Return(int64(0), errors.New("db error"))

	logger := historypkg.NewLogger(repo)

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(logger))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetHistoryStats_CountByStatusFailedError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().Count(context.Background()).Return(int64(5), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusSuccess).Return(int64(3), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusFailed).Return(int64(0), errors.New("db error"))

	logger := historypkg.NewLogger(repo)

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(logger))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetHistoryStats_CountByStatusRevertedError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().Count(context.Background()).Return(int64(5), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusSuccess).Return(int64(3), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusFailed).Return(int64(1), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusReverted).Return(int64(0), errors.New("db error"))

	logger := historypkg.NewLogger(repo)

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(logger))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetHistoryStats_CountByOperationDownloadError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().Count(context.Background()).Return(int64(5), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusSuccess).Return(int64(3), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusFailed).Return(int64(1), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusReverted).Return(int64(1), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpScrape).Return(int64(2), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpOrganize).Return(int64(1), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpDownload).Return(int64(0), errors.New("db error"))

	logger := historypkg.NewLogger(repo)

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(logger))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetHistoryStats_CountByOperationNFOError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	repo.EXPECT().Count(context.Background()).Return(int64(5), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusSuccess).Return(int64(3), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusFailed).Return(int64(1), nil)
	repo.EXPECT().CountByStatus(context.Background(), models.HistoryStatusReverted).Return(int64(1), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpScrape).Return(int64(2), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpOrganize).Return(int64(1), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpDownload).Return(int64(1), nil)
	repo.EXPECT().CountByOperation(context.Background(), models.HistoryOpNFO).Return(int64(0), errors.New("db error"))

	logger := historypkg.NewLogger(repo)

	router := gin.New()
	router.GET("/api/v1/history/stats", getHistoryStats(logger))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ---------------------------------------------------------------------------
// deleteHistory: overflow ID test
// ---------------------------------------------------------------------------

func TestDeleteHistory_OverflowID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	router := gin.New()
	router.DELETE("/api/v1/history/:id", deleteHistory(repo))

	// Value exceeds uint32 max — ParseUint with bitSize=32 should fail
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/history/%d", int64(1<<32)+1), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Invalid history ID", resp.Error)
}

// ---------------------------------------------------------------------------
// deleteHistoryBulk: movie_id takes precedence over older_than_days
// ---------------------------------------------------------------------------

func TestDeleteHistoryBulk_MovieIDTakesPrecedenceOverOlderThanDays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMockRepo(t)

	// When both params are provided, movie_id path is taken first
	repo.EXPECT().FindByMovieID(context.Background(), "ABC-001").Return([]models.History{{ID: 1}}, nil)
	repo.EXPECT().DeleteByMovieID(context.Background(), "ABC-001").Return(nil)

	router := gin.New()
	router.DELETE("/api/v1/history", deleteHistoryBulk(repo))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/history?movie_id=ABC-001&older_than_days=30", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DeleteHistoryBulkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(1), resp.Deleted)
}
