package token

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListTokensClosedDatabaseFailsWithoutPartialResponse(t *testing.T) {
	repo, db := setupHandlerTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	rec := httptest.NewRecorder()
	setupHandlerRouter(repo).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tokens", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "database is closed")
	require.NotContains(t, rec.Body.String(), `"tokens":`, "repository failure must not look like an empty successful token list")
	require.JSONEq(t, `{"error":"internal server error"}`, rec.Body.String())
}
