package actress

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteActressMergeError_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{
			name:           "invalid ID",
			err:            database.ErrActressMergeInvalidID,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "same ID",
			err:            database.ErrActressMergeSameID,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid field",
			err:            database.ErrActressMergeInvalidField,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid decision",
			err:            database.ErrActressMergeInvalidDecision,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "not found",
			err:            database.ErrNotFound,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "unique constraint",
			err:            database.ErrActressMergeUniqueConstraint,
			expectedStatus: http.StatusConflict,
		},
		{
			name:           "stale plan",
			err:            database.ErrActressMergeStalePlan,
			expectedStatus: http.StatusConflict,
		},
		{
			name:           "generic error",
			err:            errors.New("something went wrong"),
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			writeActressMergeError(c, tt.err)
			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

type versionlessActressRepo struct {
	database.ActressRepositoryInterface
}

func TestMergeActressesRequiresVersionedRepository(t *testing.T) {
	repo := newMockActressRepo()
	wrapped := &versionlessActressRepo{ActressRepositoryInterface: repo}
	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: wrapped}}))
	body := `{"target_id":1,"source_id":2,"target_updated_at":"2026-01-01T00:00:00Z","source_updated_at":"2026-01-01T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/actresses/merge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	require.Equal(t, http.StatusInternalServerError, response.Code)
}
func TestMergeActressesRequiresPreviewVersions(t *testing.T) {
	repo := newMockActressRepo()
	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
	req := httptest.NewRequest(http.MethodPost, "/actresses/merge", strings.NewReader(`{"target_id":1,"source_id":2,"target_updated_at":"0001-01-01T00:00:00Z","source_updated_at":"0001-01-01T00:00:00Z"}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	require.Equal(t, http.StatusBadRequest, response.Code)
}
