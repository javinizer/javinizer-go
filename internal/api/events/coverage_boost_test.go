package events

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/stretchr/testify/assert"
)

// --- eventStats: error branch when GetStats fails ---

func TestEventStats_RepoError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := mocks.NewMockEventRepositoryInterface(t)
	// eventlog.GetStats calls repo.Count first
	mockRepo.EXPECT().Count(context.Background()).Return(int64(0), errors.New("db unavailable"))

	router := gin.New()
	router.GET("/events/stats", eventStats(mockRepo))

	req := httptest.NewRequest("GET", "/events/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "Failed to count events")
}
