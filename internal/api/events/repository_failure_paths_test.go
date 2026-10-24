package events

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type failingEventRepository struct {
	database.EventRepositoryInterface
	fail       string
	countCalls int
	cutoff     time.Time
}

func (r *failingEventRepository) FindFiltered(context.Context, database.EventFilter, int, int) ([]models.Event, error) {
	if r.fail == "find" {
		return nil, errors.New("private database failure")
	}
	return nil, nil
}
func (r *failingEventRepository) CountFiltered(context.Context, database.EventFilter) (int64, error) {
	r.countCalls++
	if r.fail == "count" {
		return 0, errors.New("private database failure")
	}
	return 0, nil
}
func (r *failingEventRepository) DeleteOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	r.cutoff = cutoff
	return 0, errors.New("private database failure")
}

func TestListEventsRepositoryFailuresAndEmptyResult(t *testing.T) {
	for _, tc := range []struct {
		fail       string
		status     int
		message    string
		countCalls int
	}{
		{"find", http.StatusInternalServerError, "Failed to retrieve events", 0},
		{"count", http.StatusInternalServerError, "Failed to count events", 1},
		{"", http.StatusOK, "", 1},
	} {
		t.Run(tc.fail, func(t *testing.T) {
			repo := &failingEventRepository{fail: tc.fail}
			router := gin.New()
			router.GET("/events", listEvents(repo))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/events", nil))
			require.Equal(t, tc.status, rec.Code)
			require.Equal(t, tc.countCalls, repo.countCalls)
			require.NotContains(t, rec.Body.String(), "private database failure")
			if tc.fail != "" {
				require.Contains(t, rec.Body.String(), tc.message)
				return
			}
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.JSONEq(t, `[]`, string(body["events"]))
			require.JSONEq(t, `0`, string(body["total"]))
		})
	}
}
func TestDeleteEventsRepositoryFailure(t *testing.T) {
	repo := &failingEventRepository{}
	router := gin.New()
	router.DELETE("/events", deleteEvents(repo))
	before := time.Now().AddDate(0, 0, -7)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/events?older_than_days=7", nil))
	after := time.Now().AddDate(0, 0, -7)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "Failed to delete events")
	require.NotContains(t, rec.Body.String(), "private database failure")
	require.False(t, repo.cutoff.Before(before))
	require.False(t, repo.cutoff.After(after))
}
