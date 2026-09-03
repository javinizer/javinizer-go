package genre

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Spec: unknown modes are rejected before any store call.
func TestCreateWordReplacement_RejectsUnknownMatchMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockWordReplacementRepositoryInterface(t)
	// Validation precedes even the FindByOriginal lookup.

	deps := NewGenreDeps(database.ReplacementRepos{WordReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/create", createWordReplacement(deps, func() {}))

	body, _ := json.Marshal(map[string]string{"original": "X", "replacement": "Y", "match_mode": "regex"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// Spec scenario "update without mode preserves it": PUT with no match_mode
// keeps the stored wildcard mode; with an explicit mode, it switches.
func TestUpdateWordReplacement_PreservesModeWhenOmitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stored := &models.WordReplacement{ID: 7, Original: "チ*ポ", Replacement: "old", MatchMode: models.MatchModeWildcard}
	preserved := *stored
	preserved.Replacement = "new"

	mockRepo := mocks.NewMockWordReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "チ*ポ").Return(stored, nil)
	mockRepo.EXPECT().Upsert(context.Background(), &preserved).Return(nil)

	deps := NewGenreDeps(database.ReplacementRepos{WordReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.PUT("/update", updateWordReplacement(deps, func() {}))

	body, _ := json.Marshal(map[string]string{"original": "チ*ポ", "replacement": "new"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// PUT with an unknown mode is a 400 before any store call.
func TestUpdateWordReplacement_RejectsUnknownMatchMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockWordReplacementRepositoryInterface(t)

	deps := NewGenreDeps(database.ReplacementRepos{WordReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.PUT("/update", updateWordReplacement(deps, func() {}))

	body, _ := json.Marshal(map[string]string{"original": "チ*ポ", "replacement": "new", "match_mode": "regex"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// PUT with an explicit mode switches it (wildcard back to literal).
func TestUpdateWordReplacement_ExplicitModeSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stored := &models.WordReplacement{ID: 9, Original: "チ*ポ", Replacement: "x", MatchMode: models.MatchModeWildcard}
	want := *stored
	want.MatchMode = models.MatchModeLiteral

	mockRepo := mocks.NewMockWordReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "チ*ポ").Return(stored, nil)
	mockRepo.EXPECT().Upsert(context.Background(), &want).Return(nil)

	deps := NewGenreDeps(database.ReplacementRepos{WordReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.PUT("/update", updateWordReplacement(deps, func() {}))

	body, _ := json.Marshal(map[string]string{"original": "チ*ポ", "replacement": "x", "match_mode": "literal"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// Spec scenario "re-import updates mode only": same original+replacement with
// a different mode must count as an update, not a skip.
func TestImportWordReplacements_ModeOnlyChangeIsImported(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stored := &models.WordReplacement{ID: 3, Original: "チ○ポ", Replacement: "チンポ", MatchMode: models.MatchModeLiteral}
	updated := *stored
	updated.MatchMode = models.MatchModeWildcard

	mockRepo := mocks.NewMockWordReplacementRepositoryInterface(t)
	mockRepo.EXPECT().FindByOriginal(context.Background(), "チ○ポ").Return(stored, nil)
	mockRepo.EXPECT().Upsert(context.Background(), &updated).Return(nil)

	deps := NewGenreDeps(database.ReplacementRepos{WordReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/import", importWordReplacements(deps, func() {}))

	body, _ := json.Marshal(map[string]interface{}{
		"replacements": []map[string]string{{"original": "チ○ポ", "replacement": "チンポ", "match_mode": "wildcard"}},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var summary importSummaryResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &summary))
	assert.Equal(t, 1, summary.Imported)
	assert.Equal(t, 0, summary.Skipped)
}

// Import with an unknown per-item mode counts an error, never stores.
func TestImportWordReplacements_UnknownModeIsPerItemError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := mocks.NewMockWordReplacementRepositoryInterface(t)

	deps := NewGenreDeps(database.ReplacementRepos{WordReplacementRepo: mockRepo}, database.TranslationRepos{})
	router := gin.New()
	router.POST("/import", importWordReplacements(deps, func() {}))

	body, _ := json.Marshal(map[string]interface{}{
		"replacements": []map[string]string{{"original": "A", "replacement": "B", "match_mode": "bogus"}},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var summary importSummaryResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &summary))
	assert.Equal(t, 1, summary.Errors)
	assert.Equal(t, 0, summary.Imported)
}
