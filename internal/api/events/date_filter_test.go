package events

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListEvents_DateFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupEventsTestDeps(t)
	defer func() { _ = db.Close() }()

	e1 := &models.Event{
		EventType: models.EventCategoryScraper, Severity: models.SeverityError,
		Message: "4/13 10:00Z", Source: "test",
		CreatedAt: time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC),
	}
	e2 := &models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityInfo,
		Message: "4/13 16:00Z", Source: "test",
		CreatedAt: time.Date(2026, 4, 13, 16, 0, 0, 0, time.UTC),
	}
	e3 := &models.Event{
		EventType: models.EventCategoryScraper, Severity: models.SeverityInfo,
		Message: "4/14 03:00Z", Source: "test",
		CreatedAt: time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC),
	}
	e4 := &models.Event{
		EventType: models.EventCategoryOrganize, Severity: models.SeverityWarn,
		Message: "4/14 08:00Z", Source: "test",
		CreatedAt: time.Date(2026, 4, 14, 8, 0, 0, 0, time.UTC),
	}

	for _, e := range []*models.Event{e1, e2, e3, e4} {
		require.NoError(t, deps.EventRepo.Create(e))
	}

	router := gin.New()
	router.GET("/api/v1/events", listEvents(deps))

	tests := []struct {
		name          string
		queryParams   string
		expectedTotal int64
	}{
		{
			name:          "no filter returns all",
			queryParams:   "",
			expectedTotal: 4,
		},
		{
			// EST (UTC-4): user sets 4/14 midnight local -> API sends 2026-04-14T04:00:00Z
			// Filters: created_at >= 2026-04-14T04:00:00Z
			// Includes: 4/14 08:00Z (only event after 04:00 UTC on 4/14)
			name:          "EST start=4/14 midnight local",
			queryParams:   "?start=2026-04-14T04:00:00Z",
			expectedTotal: 1,
		},
		{
			// JST (UTC+9): user sets 4/14 midnight local -> API sends 2026-04-13T15:00:00Z
			// Filters: created_at >= 2026-04-13T15:00:00Z
			// Includes: 4/13 16:00Z, 4/14 03:00Z, 4/14 08:00Z (all after 15:00 UTC on 4/13)
			// Excludes: 4/13 10:00Z (before 15:00 UTC)
			name:          "JST start=4/14 midnight local",
			queryParams:   "?start=2026-04-13T15:00:00Z",
			expectedTotal: 3,
		},
		{
			// Start at UTC midnight 4/14
			// Filters: created_at >= 2026-04-14T00:00:00Z
			// Includes: 4/14 03:00Z, 4/14 08:00Z
			// Excludes: 4/13 10:00Z, 4/13 16:00Z
			name:          "start=4/14 UTC midnight",
			queryParams:   "?start=2026-04-14T00:00:00Z",
			expectedTotal: 2,
		},
		{
			// End filter: events before 4/14 midnight UTC
			// Filters: created_at < 2026-04-14T00:00:00Z
			// Includes: 4/13 10:00Z, 4/13 16:00Z (all events before midnight 4/14)
			name:          "end=4/14 UTC midnight",
			queryParams:   "?end=2026-04-14T00:00:00Z",
			expectedTotal: 2,
		},
		{
			// Range filter: from 4/13 12:00 to 4/14 06:00 UTC
			// Includes: 4/13 16:00Z, 4/14 03:00Z
			name:          "range 4/13 12:00 to 4/14 06:00 UTC",
			queryParams:   "?start=2026-04-13T12:00:00Z&end=2026-04-14T06:00:00Z",
			expectedTotal: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/events"+tt.queryParams, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var resp eventListResponse
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedTotal, resp.Total)
		})
	}
}
