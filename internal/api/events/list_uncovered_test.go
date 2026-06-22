package events

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListEvents_InvalidSeverity_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupEventsTestDeps(t)
	defer func() { _ = db.Close() }()

	router := gin.New()
	router.GET("/api/v1/events", listEvents(deps.Repos.EventRepo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?severity=critical", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "Invalid severity filter")
}

func TestListEvents_InvalidType_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupEventsTestDeps(t)
	defer func() { _ = db.Close() }()

	router := gin.New()
	router.GET("/api/v1/events", listEvents(deps.Repos.EventRepo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?type=invalid_type", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "Invalid type filter")
}

func TestListEvents_EmptySourceFilter_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupEventsTestDeps(t)
	defer func() { _ = db.Close() }()

	router := gin.New()
	router.GET("/api/v1/events", listEvents(deps.Repos.EventRepo))

	// Source filter with only whitespace should return 400
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?source=%20%20%20", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListEvents_ValidSeverityAndType_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupEventsTestDeps(t)
	defer func() { _ = db.Close() }()

	router := gin.New()
	router.GET("/api/v1/events", listEvents(deps.Repos.EventRepo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?type=scraper&severity=error", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListEvents_InvalidEndDateFormat_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupEventsTestDeps(t)
	defer func() { _ = db.Close() }()

	router := gin.New()
	router.GET("/api/v1/events", listEvents(deps.Repos.EventRepo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?end=not-a-date", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "Invalid end date format")
}
