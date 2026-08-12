package actress

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteActressMergeError_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
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
			expectedCode:   "ACTRESS_MERGE_STALE_PLAN",
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
			if tt.expectedCode != "" {
				assert.Contains(t, w.Body.String(), tt.expectedCode)
			}
		})
	}
}

// postActressMerge wires the merge endpoint to a real actress repository and
// POSTs body after substituting the created actresses' IDs and timestamps.
func postActressMerge(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := newMockActressRepo()
	target := models.Actress{DMMID: 100, FirstName: "Target"}
	require.NoError(t, repo.Create(context.Background(), &target))
	source := models.Actress{DMMID: 200, FirstName: "Source"}
	require.NoError(t, repo.Create(context.Background(), &source))
	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
	replacer := strings.NewReplacer(
		`"@TARGET@"`, strconv.FormatUint(uint64(target.ID), 10),
		`"@SOURCE@"`, strconv.FormatUint(uint64(source.ID), 10),
		`"@TARGET_TS@"`, strconv.Quote(target.UpdatedAt.UTC().Format(time.RFC3339Nano)),
		`"@SOURCE_TS@"`, strconv.Quote(source.UpdatedAt.UTC().Format(time.RFC3339Nano)),
	)
	req := httptest.NewRequest(http.MethodPost, "/actresses/merge", strings.NewReader(replacer.Replace(body)))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

// The handler goes through ActressRepositoryInterface, whose contract now
// includes versioned merges.
func TestMergeActressesMergesThroughRepositoryInterface(t *testing.T) {
	response := postActressMerge(t, `{"target_id":"@TARGET@","source_id":"@SOURCE@","target_updated_at":"@TARGET_TS@","source_updated_at":"@SOURCE_TS@"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "merged_from_id")
}

// Legacy clients that omit the preview timestamps keep working; the merge is
// then unfenced (pre-versioning behavior).
func TestMergeActressesAllowsMissingPreviewVersions(t *testing.T) {
	response := postActressMerge(t, `{"target_id":"@TARGET@","source_id":"@SOURCE@"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "merged_from_id")
}
