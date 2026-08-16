package actress

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCovFinal_CandidatesBadLimit(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-candidates?limit=abc", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCovFinal_CandidatesNegativeLimit(t *testing.T) {
	router, _ := setupSyncTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses/sync-candidates?limit=-5", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
