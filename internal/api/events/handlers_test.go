package events

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupEventsTestDeps(t *testing.T) (*ServerDependencies, *database.DB) {
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate())

	eventRepo := database.NewEventRepository(db)

	deps := &ServerDependencies{
		DB:        db,
		EventRepo: eventRepo,
	}
	deps.SetConfig(cfg)

	return deps, db
}

func seedEventsData(t *testing.T, deps *ServerDependencies) {
	t.Helper()

	events := []*models.Event{
		{EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Message: "Scrape succeeded", Source: "r18dev", CreatedAt: time.Now().Add(-2 * time.Hour)},
		{EventType: models.EventCategoryScraper, Severity: models.SeverityError, Message: "Scrape failed", Source: "dmm", CreatedAt: time.Now().Add(-1 * time.Hour)},
		{EventType: models.EventCategoryOrganize, Severity: models.SeverityInfo, Message: "Organize completed", Source: "organizer", CreatedAt: time.Now().Add(-30 * time.Minute)},
		{EventType: models.EventCategoryOrganize, Severity: models.SeverityWarn, Message: "File conflict", Source: "organizer", CreatedAt: time.Now().Add(-20 * time.Minute)},
		{EventType: models.EventCategorySystem, Severity: models.SeverityDebug, Message: "Server started", Source: "server", CreatedAt: time.Now().Add(-10 * time.Minute)},
		{EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "Config reloaded", Source: "server", CreatedAt: time.Now().Add(-5 * time.Minute)},
	}

	for _, e := range events {
		require.NoError(t, deps.EventRepo.Create(e))
	}
}

func TestListEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		queryParams    string
		seedData       bool
		expectedStatus int
		validateFn     func(*testing.T, *eventListResponse)
	}{
		{
			name:           "empty events list",
			queryParams:    "",
			seedData:       false,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				assert.Empty(t, resp.Events)
				assert.Equal(t, int64(0), resp.Total)
			},
		},
		{
			name:           "list all events",
			queryParams:    "",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				assert.Len(t, resp.Events, 6)
				assert.Equal(t, int64(6), resp.Total)
			},
		},
		{
			name:           "filter by type scraper",
			queryParams:    "?type=scraper",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				assert.Len(t, resp.Events, 2)
				for _, e := range resp.Events {
					assert.Equal(t, models.EventCategoryScraper, e.EventType)
				}
			},
		},
		{
			name:           "filter by severity error",
			queryParams:    "?severity=error",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				assert.Len(t, resp.Events, 1)
				assert.Equal(t, models.SeverityError, resp.Events[0].Severity)
			},
		},
		{
			name:           "filter by type and severity",
			queryParams:    "?type=scraper&severity=error",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				assert.Len(t, resp.Events, 1)
				assert.Equal(t, models.EventCategoryScraper, resp.Events[0].EventType)
				assert.Equal(t, models.SeverityError, resp.Events[0].Severity)
			},
		},
		{
			name:           "invalid type filter",
			queryParams:    "?type=invalid",
			seedData:       false,
			expectedStatus: http.StatusBadRequest,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				// Should not reach here — error response is different shape
			},
		},
		{
			name:           "invalid severity filter",
			queryParams:    "?severity=critical",
			seedData:       false,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "filter by source r18dev",
			queryParams:    "?source=r18dev",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				assert.Len(t, resp.Events, 1)
				assert.Equal(t, "r18dev", resp.Events[0].Source)
			},
		},
		{
			name:           "filter by source organizer",
			queryParams:    "?source=organizer",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				assert.Len(t, resp.Events, 2)
				for _, e := range resp.Events {
					assert.Equal(t, "organizer", e.Source)
				}
			},
		},
		{
			name:           "filter by source nonexistent",
			queryParams:    "?source=nonexistent",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				assert.Empty(t, resp.Events)
				assert.Equal(t, int64(0), resp.Total)
			},
		},
		{
			name:           "empty source filter rejected",
			queryParams:    "?source=+",
			seedData:       false,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "composable filter: type + source",
			queryParams:    "?type=organize&source=organizer",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				assert.Len(t, resp.Events, 2)
				for _, e := range resp.Events {
					assert.Equal(t, models.EventCategoryOrganize, e.EventType)
					assert.Equal(t, "organizer", e.Source)
				}
			},
		},
		{
			name:           "composable filter: severity + source",
			queryParams:    "?severity=info&source=r18dev",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventListResponse) {
				assert.Len(t, resp.Events, 1)
				assert.Equal(t, models.SeverityInfo, resp.Events[0].Severity)
				assert.Equal(t, "r18dev", resp.Events[0].Source)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, db := setupEventsTestDeps(t)
			defer func() { _ = db.Close() }()

			if tt.seedData {
				seedEventsData(t, deps)
			}

			router := gin.New()
			router.GET("/api/v1/events", listEvents(deps))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK && tt.validateFn != nil {
				var resp eventListResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}

func TestEventStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		seedData       bool
		expectedStatus int
		validateFn     func(*testing.T, *eventStatsResponse)
	}{
		{
			name:           "empty stats",
			seedData:       false,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventStatsResponse) {
				assert.Equal(t, int64(0), resp.Total)
				assert.Equal(t, int64(0), resp.ByType[models.EventCategoryScraper])
				assert.Equal(t, int64(0), resp.ByType[models.EventCategoryOrganize])
				assert.Equal(t, int64(0), resp.ByType[models.EventCategorySystem])
				assert.Empty(t, resp.BySource)
			},
		},
		{
			name:           "with seeded data",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *eventStatsResponse) {
				assert.Equal(t, int64(6), resp.Total)
				assert.Equal(t, int64(2), resp.ByType[models.EventCategoryScraper])
				assert.Equal(t, int64(2), resp.ByType[models.EventCategoryOrganize])
				assert.Equal(t, int64(2), resp.ByType[models.EventCategorySystem])
				assert.Equal(t, int64(1), resp.BySeverity[models.SeverityDebug])
				assert.Equal(t, int64(3), resp.BySeverity[models.SeverityInfo])
				assert.Equal(t, int64(1), resp.BySeverity[models.SeverityWarn])
				assert.Equal(t, int64(1), resp.BySeverity[models.SeverityError])
				assert.Equal(t, int64(1), resp.BySource["r18dev"])
				assert.Equal(t, int64(1), resp.BySource["dmm"])
				assert.Equal(t, int64(2), resp.BySource["organizer"])
				assert.Equal(t, int64(2), resp.BySource["server"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, db := setupEventsTestDeps(t)
			defer func() { _ = db.Close() }()

			if tt.seedData {
				seedEventsData(t, deps)
			}

			router := gin.New()
			router.GET("/api/v1/events/stats", eventStats(deps))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stats", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil {
				var resp eventStatsResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}

func TestEventStats_ByType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupEventsTestDeps(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, deps.EventRepo.Create(&models.Event{
		EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Message: "scrape 1", Source: "r18dev", CreatedAt: time.Now(),
	}))
	require.NoError(t, deps.EventRepo.Create(&models.Event{
		EventType: models.EventCategoryScraper, Severity: models.SeverityError, Message: "scrape 2", Source: "dmm", CreatedAt: time.Now(),
	}))
	require.NoError(t, deps.EventRepo.Create(&models.Event{
		EventType: models.EventCategoryOrganize, Severity: models.SeverityWarn, Message: "organize 1", Source: "organizer", CreatedAt: time.Now(),
	}))
	require.NoError(t, deps.EventRepo.Create(&models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityDebug, Message: "system 1", Source: "server", CreatedAt: time.Now(),
	}))

	router := gin.New()
	router.GET("/api/v1/events/stats", eventStats(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp eventStatsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(4), resp.Total)
	assert.Equal(t, int64(2), resp.ByType[models.EventCategoryScraper])
	assert.Equal(t, int64(1), resp.ByType[models.EventCategoryOrganize])
	assert.Equal(t, int64(1), resp.ByType[models.EventCategorySystem])
}

func TestListEvents_InvalidDateFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupEventsTestDeps(t)
	defer func() { _ = db.Close() }()

	router := gin.New()
	router.GET("/api/v1/events", listEvents(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?start=invalid-date", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		queryParams    string
		seedData       bool
		expectedStatus int
		validateFn     func(*testing.T, *deleteEventsResponse)
	}{
		{
			name:           "missing older_than_days parameter",
			queryParams:    "",
			seedData:       false,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid older_than_days zero",
			queryParams:    "?older_than_days=0",
			seedData:       false,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid older_than_days negative",
			queryParams:    "?older_than_days=-5",
			seedData:       false,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "valid delete with seeded data",
			queryParams:    "?older_than_days=1",
			seedData:       true,
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *deleteEventsResponse) {
				// All seeded events are within the last 2 hours, so none should be deleted with 1 day
				assert.Equal(t, int64(0), resp.Deleted)
				assert.Contains(t, resp.Message, "1 days")
			},
		},
		{
			name:           "delete old events",
			queryParams:    "?older_than_days=1",
			seedData:       false, // manually seed old events
			expectedStatus: http.StatusOK,
			validateFn: func(t *testing.T, resp *deleteEventsResponse) {
				assert.True(t, resp.Deleted > 0, "should have deleted old events")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, db := setupEventsTestDeps(t)
			defer func() { _ = db.Close() }()

			if tt.seedData {
				seedEventsData(t, deps)
			}

			// For "delete old events" test, manually seed old events
			if tt.name == "delete old events" {
				oldEvent := &models.Event{
					EventType: models.EventCategorySystem,
					Severity:  models.SeverityInfo,
					Message:   "Old event",
					Source:    "server",
					CreatedAt: time.Now().Add(-48 * time.Hour), // 2 days ago
				}
				require.NoError(t, deps.EventRepo.Create(oldEvent))
			}

			router := gin.New()
			router.DELETE("/api/v1/events", deleteEvents(deps))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/events"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK && tt.validateFn != nil {
				var resp deleteEventsResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}
