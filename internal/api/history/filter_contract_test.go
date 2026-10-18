package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetHistoryCombinedFiltersRepository(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, repo := setupHistoryTestDB(t)
	defer db.Close()
	var rows []models.History
	// Every predicate has independent distractors; timestamps make ordering explicit.
	for _, movie := range []string{"A", "B"} {
		for _, op := range []models.HistoryOperation{models.HistoryOpScrape, models.HistoryOpOrganize} {
			for _, status := range []models.HistoryStatus{models.HistoryStatusFailed, models.HistoryStatusSuccess} {
				for range 3 {
					row := models.History{MovieID: movie, Operation: op, Status: status, CreatedAt: time.Date(2026, 1, 1, 0, 0, len(rows), 0, time.UTC)}
					require.NoError(t, repo.Create(context.Background(), &row))
					rows = append(rows, row)
				}
			}
		}
	}
	router := gin.New()
	router.GET("/history", getHistory(repo))
	for mask := 0; mask < 8; mask++ {
		t.Run(fmt.Sprint(mask), func(t *testing.T) {
			params := url.Values{}
			if mask&1 != 0 {
				params.Set("movie_id", "A")
			}
			if mask&2 != 0 {
				params.Set("operation", "scrape")
			}
			if mask&4 != 0 {
				params.Set("status", "failed")
			}
			var ids []uint
			for i := len(rows) - 1; i >= 0; i-- {
				row := rows[i]
				if mask&1 != 0 && row.MovieID != "A" || mask&2 != 0 && row.Operation != models.HistoryOpScrape || mask&4 != 0 && row.Status != models.HistoryStatusFailed {
					continue
				}
				ids = append(ids, row.ID)
			}
			for _, offset := range []int{0, 1, len(ids), len(ids) + 10} {
				params.Set("limit", "2")
				params.Set("offset", fmt.Sprint(offset))
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/history?"+params.Encode(), nil))
				require.Equal(t, http.StatusOK, w.Code)
				var got HistoryListResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
				assert.Equal(t, int64(len(ids)), got.Total)
				assert.Equal(t, 2, got.Limit)
				assert.Equal(t, offset, got.Offset)
				start := min(offset, len(ids))
				end := min(start+2, len(ids))
				actual := make([]uint, 0, len(got.Records))
				for _, row := range got.Records {
					actual = append(actual, row.ID)
				}
				assert.Equal(t, ids[start:end], actual)
				if start == len(ids) {
					assert.Contains(t, w.Body.String(), `"records":[]`)
				}
			}
		})
	}
	for _, query := range []string{"movie_id=missing", "operation=unknown", "status=unknown", "movie_id=A&operation=unknown&status=failed"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/history?"+query, nil))
		require.Equal(t, 200, w.Code)
		var got HistoryListResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Zero(t, got.Total)
		assert.Empty(t, got.Records)
	}
}

func TestDeleteHistoryLookupClassification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		err     error
		code    int
		message string
	}{
		{"missing", database.ErrNotFound, 404, "History record not found"},
		{"wrapped missing", fmt.Errorf("lookup: %w", database.ErrNotFound), 404, "History record not found"},
		{"gorm missing", gorm.ErrRecordNotFound, 404, "History record not found"},
		{"database failure", errors.New("private database details"), 500, retrieveHistoryErrorMessage},
		{"canceled", context.Canceled, 500, retrieveHistoryErrorMessage},
		{"deadline", context.DeadlineExceeded, 500, retrieveHistoryErrorMessage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockRepo(t)
			repo.EXPECT().FindByID(context.Background(), uint(42)).Return(nil, tc.err).Once()
			router := gin.New()
			router.DELETE("/history/:id", deleteHistory(repo))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/history/42", nil))
			assert.Equal(t, tc.code, w.Code)
			assert.JSONEq(t, fmt.Sprintf(`{"error":%q}`, tc.message), w.Body.String())
			repo.AssertNotCalled(t, "Delete")
		})
	}
}
