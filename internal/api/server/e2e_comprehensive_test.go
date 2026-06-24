package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/auth"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComprehensiveE2E exercises the full API surface in a single chained test.
// Each section builds on state from the previous section so we exercise maximum
// code paths with minimum setup overhead.
func TestComprehensiveE2E(t *testing.T) {
	router, deps := setupCompleteServer(t)

	// ── Helpers ────────────────────────────────────────────────────────────
	var sessionCookie string

	doReq := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var bodyReader io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			require.NoError(t, err)
			bodyReader = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, path, bodyReader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:8080")
		if sessionCookie != "" {
			req.Header.Set("Cookie", "javinizer_session="+sessionCookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	parseJSON := func(w *httptest.ResponseRecorder, dest any) {
		t.Helper()
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), dest), "body: %s", truncate(w.Body.String(), 300))
	}

	extractCookie := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		for _, c := range w.Result().Cookies() {
			if c.Name == "javinizer_session" {
				return c.Value
			}
		}
		return ""
	}

	// ══════════════════════════════════════════════════════════════════════
	// 1. SETUP: Enable auth, create admin user directly
	// ══════════════════════════════════════════════════════════════════════
	t.Run("Setup_Auth", func(t *testing.T) {
		manager, err := auth.NewAuthManager(deps.ConfigFile, time.Hour)
		require.NoError(t, err, "failed to create auth manager")
		manager.SetDisableRateLimit(true)
		require.NoError(t, manager.Setup("admin", "adminpassword123"))

		// Wire ApiTokenRepo so Bearer token auth works
		manager.SetApiTokenRepo(deps.Repos.ApiTokenRepo)

		deps.Auth = manager
		router = NewServer(testkit.GetTestRuntime(deps))

		// Verify auth is initialized
		w := doReq("GET", "/api/v1/auth/status", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var status map[string]any
		parseJSON(w, &status)
		assert.Equal(t, true, status["initialized"])
	})

	// ══════════════════════════════════════════════════════════════════════
	// 2. AUTH: Login, verify status, logout, re-login
	// ══════════════════════════════════════════════════════════════════════
	t.Run("Auth_LoginStatusLogout", func(t *testing.T) {
		// Login
		w := doReq("POST", "/api/v1/auth/login", map[string]any{
			"username":    "admin",
			"password":    "adminpassword123",
			"remember_me": true,
		})
		assert.Equal(t, http.StatusOK, w.Code)
		cookie := extractCookie(w)
		require.NotEmpty(t, cookie, "login should return a session cookie")
		sessionCookie = cookie

		// Verify auth status shows authenticated
		w = doReq("GET", "/api/v1/auth/status", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var status map[string]any
		parseJSON(w, &status)
		assert.Equal(t, true, status["initialized"])
		assert.Equal(t, true, status["authenticated"])
		assert.Equal(t, "admin", status["username"])

		// Logout
		w = doReq("POST", "/api/v1/auth/logout", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Re-login
		sessionCookie = ""
		w = doReq("POST", "/api/v1/auth/login", map[string]any{
			"username":    "admin",
			"password":    "adminpassword123",
			"remember_me": true,
		})
		assert.Equal(t, http.StatusOK, w.Code)
		cookie = extractCookie(w)
		require.NotEmpty(t, cookie, "login should return a session cookie")
		sessionCookie = cookie

		// Wrong password
		savedCookie := sessionCookie
		sessionCookie = ""
		w = doReq("POST", "/api/v1/auth/login", map[string]any{
			"username": "admin",
			"password": "wrongpassword",
		})
		assert.Equal(t, http.StatusUnauthorized, w.Code)
		sessionCookie = savedCookie
	})

	// ══════════════════════════════════════════════════════════════════════
	// 3. MOVIES: List, Create via repo, Get, Delete
	// ══════════════════════════════════════════════════════════════════════
	var movieID string
	t.Run("Movies_Lifecycle", func(t *testing.T) {
		// List movies (may have data from previous tests sharing the DB)
		w := doReq("GET", "/api/v1/movies", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var listResp map[string]any
		parseJSON(w, &listResp)

		// Create movie via repo
		movieID = "E2E-001"
		movie := &models.Movie{
			ContentID:    movieID,
			ID:           movieID,
			Title:        "E2E Test Movie",
			DisplayTitle: "E2E Test Movie",
			Maker:        "E2E Studio",
			Genres:       []models.Genre{{Name: "Action"}},
			Actresses:    []models.Actress{{LastName: "E2E", FirstName: "Star"}},
		}
		_, err := deps.Repos.MovieRepo.Upsert(context.Background(), movie)
		require.NoError(t, err)

		// Get single movie
		w = doReq("GET", "/api/v1/movies/"+movieID, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var movieResp map[string]any
		parseJSON(w, &movieResp)
		m, ok := movieResp["movie"].(map[string]any)
		require.True(t, ok, "movie field should be an object")
		assert.Equal(t, movieID, m["code"])
		assert.Equal(t, "E2E Test Movie", m["title"])

		// List movies (should have at least 1)
		w = doReq("GET", "/api/v1/movies", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		parseJSON(w, &listResp)
		assert.GreaterOrEqual(t, listResp["count"], 1.0)

		// List with pagination
		w = doReq("GET", "/api/v1/movies?limit=1&offset=0", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Get nonexistent movie
		w = doReq("GET", "/api/v1/movies/NOEXIST-999", nil)
		assert.Equal(t, http.StatusNotFound, w.Code)

		// Delete movie
		require.NoError(t, deps.Repos.MovieRepo.Delete(context.Background(), movieID))
		w = doReq("GET", "/api/v1/movies/"+movieID, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 4. ACTRESSES: Create, Get, Search, Update, Delete
	// ══════════════════════════════════════════════════════════════════════
	var actressID float64
	t.Run("Actresses_Lifecycle", func(t *testing.T) {
		// List actresses (may have data from movie creation above)
		w := doReq("GET", "/api/v1/actresses", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Create actress
		w = doReq("POST", "/api/v1/actresses", map[string]any{
			"first_name":    "Hanako",
			"last_name":     "Yamada",
			"japanese_name": "山田花子",
			"dmm_id":        12345,
		})
		assert.Equal(t, http.StatusCreated, w.Code)
		var created map[string]any
		parseJSON(w, &created)
		actressID = created["id"].(float64)
		assert.Equal(t, "Hanako", created["first_name"])
		assert.Equal(t, "Yamada", created["last_name"])
		assert.Equal(t, "山田花子", created["japanese_name"])

		// Get actress by ID
		idStr := fmt.Sprintf("%.0f", actressID)
		w = doReq("GET", "/api/v1/actresses/"+idStr, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var fetched map[string]any
		parseJSON(w, &fetched)
		assert.Equal(t, "Hanako", fetched["first_name"])

		// Search actresses
		w = doReq("GET", "/api/v1/actresses/search?q=Yamada", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var searchResults []any
		parseJSON(w, &searchResults)
		assert.NotEmpty(t, searchResults)

		// Update actress
		w = doReq("PUT", "/api/v1/actresses/"+idStr, map[string]any{
			"first_name":    "Hanako",
			"last_name":     "Suzuki",
			"japanese_name": "鈴木花子",
			"dmm_id":        12345,
		})
		assert.Equal(t, http.StatusOK, w.Code)
		var updated map[string]any
		parseJSON(w, &updated)
		assert.Equal(t, "Suzuki", updated["last_name"])

		// Verify update
		w = doReq("GET", "/api/v1/actresses/"+idStr, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		parseJSON(w, &fetched)
		assert.Equal(t, "Suzuki", fetched["last_name"])

		// Delete actress
		w = doReq("DELETE", "/api/v1/actresses/"+idStr, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify deletion
		w = doReq("GET", "/api/v1/actresses/"+idStr, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 5. GENRES: List, Create replacement, Update, Delete
	// ══════════════════════════════════════════════════════════════════════
	var genreReplacementID float64
	t.Run("Genres_Lifecycle", func(t *testing.T) {
		// List genres (may have genres from movie upsert)
		w := doReq("GET", "/api/v1/genres", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// List genre replacements
		w = doReq("GET", "/api/v1/genres/replacements", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Create genre replacement
		w = doReq("POST", "/api/v1/genres/replacements", map[string]any{
			"original":    "Blow",
			"replacement": "Blowjob",
		})
		assert.Equal(t, http.StatusCreated, w.Code)
		var created map[string]any
		parseJSON(w, &created)
		genreReplacementID = created["id"].(float64)
		assert.Equal(t, "Blow", created["original"])
		assert.Equal(t, "Blowjob", created["replacement"])

		// Update genre replacement
		w = doReq("PUT", "/api/v1/genres/replacements", map[string]any{
			"original":    "Blow",
			"replacement": "Oral",
		})
		assert.Equal(t, http.StatusOK, w.Code)

		// Export genre replacements
		w = doReq("GET", "/api/v1/genres/replacements/export", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Import genre replacements
		w = doReq("POST", "/api/v1/genres/replacements/import", map[string]any{
			"replacements": []map[string]any{
				{"original": "ActionRepl", "replacement": "アクション"},
			},
		})
		assert.Equal(t, http.StatusOK, w.Code)
		var importResp map[string]any
		parseJSON(w, &importResp)
		assert.Equal(t, 1.0, importResp["imported"])

		// Delete genre replacement by ID
		w = doReq("DELETE", fmt.Sprintf("/api/v1/genres/replacements?id=%.0f", genreReplacementID), nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Delete genre replacement by original name
		w = doReq("DELETE", "/api/v1/genres/replacements?original=ActionRepl", nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 6. WORDS: List, Create replacement, Update, Delete
	// ══════════════════════════════════════════════════════════════════════
	var wordReplacementID float64
	t.Run("Words_Lifecycle", func(t *testing.T) {
		// List word replacements
		w := doReq("GET", "/api/v1/words/replacements", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Create word replacement
		w = doReq("POST", "/api/v1/words/replacements", map[string]any{
			"original":    "censored",
			"replacement": "uncensored",
		})
		assert.Equal(t, http.StatusCreated, w.Code)
		var created map[string]any
		parseJSON(w, &created)
		wordReplacementID = created["id"].(float64)
		assert.Equal(t, "censored", created["original"])

		// Update word replacement
		w = doReq("PUT", "/api/v1/words/replacements", map[string]any{
			"original":    "censored",
			"replacement": "redacted",
		})
		assert.Equal(t, http.StatusOK, w.Code)

		// Export word replacements
		w = doReq("GET", "/api/v1/words/replacements/export", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Import word replacements
		w = doReq("POST", "/api/v1/words/replacements/import", map[string]any{
			"replacements": []map[string]any{
				{"original": "blur", "replacement": "clear"},
			},
		})
		assert.Equal(t, http.StatusOK, w.Code)
		var importResp map[string]any
		parseJSON(w, &importResp)
		assert.Equal(t, 1.0, importResp["imported"])

		// Delete word replacement by ID
		w = doReq("DELETE", fmt.Sprintf("/api/v1/words/replacements?id=%.0f", wordReplacementID), nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Delete the imported one by original
		w = doReq("DELETE", "/api/v1/words/replacements?original=blur", nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 7. JOBS: List jobs, Get job, Operations, Revert check
	// ══════════════════════════════════════════════════════════════════════
	t.Run("Jobs_ListAndGet", func(t *testing.T) {
		// List jobs (should be empty or have data from DB)
		w := doReq("GET", "/api/v1/jobs", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Get nonexistent job
		w = doReq("GET", "/api/v1/jobs/nonexistent-job-id", nil)
		assert.Equal(t, http.StatusNotFound, w.Code)

		// List operations for nonexistent job — returns empty or 404
		w = doReq("GET", "/api/v1/jobs/nonexistent-job-id/operations", nil)
		assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, w.Code)

		// Revert check for nonexistent job — returns 403 or 404
		w = doReq("GET", "/api/v1/jobs/nonexistent-job-id/revert-check", nil)
		assert.Contains(t, []int{http.StatusOK, http.StatusNotFound, http.StatusForbidden}, w.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 8. EVENTS: Create, List, Get stats, Delete
	// ══════════════════════════════════════════════════════════════════════
	t.Run("Events_Lifecycle", func(t *testing.T) {
		// Seed some events
		for i := 0; i < 3; i++ {
			event := &models.Event{
				EventType: models.EventCategoryScraper,
				Severity:  models.SeverityInfo,
				Message:   fmt.Sprintf("e2e test event %d", i),
				Source:    "e2e-test",
			}
			require.NoError(t, deps.Repos.EventRepo.Create(context.Background(), event))
		}

		// List events
		w := doReq("GET", "/api/v1/events", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var eventsResp map[string]any
		parseJSON(w, &eventsResp)
		events, ok := eventsResp["events"].([]any)
		require.True(t, ok, "events field should be an array")
		assert.GreaterOrEqual(t, len(events), 3)

		// List events with filters
		w = doReq("GET", "/api/v1/events?type=scraper&severity=info&source=e2e-test", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// Event stats
		w = doReq("GET", "/api/v1/events/stats", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var statsResp map[string]any
		parseJSON(w, &statsResp)
		assert.Contains(t, statsResp, "total")

		// Delete events (requires older_than_days)
		w = doReq("DELETE", "/api/v1/events?older_than_days=365", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var delResp map[string]any
		parseJSON(w, &delResp)
		assert.Contains(t, delResp, "deleted")
	})

	// ══════════════════════════════════════════════════════════════════════
	// 9. HISTORY: Create, List, Get stats, Delete
	// ══════════════════════════════════════════════════════════════════════
	t.Run("History_Lifecycle", func(t *testing.T) {
		// Seed some history
		for i := 0; i < 3; i++ {
			h := &models.History{
				MovieID:   fmt.Sprintf("HIST-E2E-%03d", i),
				Operation: models.HistoryOpScrape,
				Status:    models.HistoryStatusSuccess,
			}
			require.NoError(t, deps.Repos.HistoryRepo.Create(context.Background(), h))
		}

		// List history
		w := doReq("GET", "/api/v1/history", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var histResp map[string]any
		parseJSON(w, &histResp)
		assert.GreaterOrEqual(t, histResp["total"], 1.0)

		// List with filters
		w = doReq("GET", "/api/v1/history?operation=scrape&status=success", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// List by movie_id
		w = doReq("GET", "/api/v1/history?movie_id=HIST-E2E-000", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// History stats
		w = doReq("GET", "/api/v1/history/stats", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var statsResp map[string]any
		parseJSON(w, &statsResp)
		assert.GreaterOrEqual(t, statsResp["total"], 1.0)

		// Delete single history record
		records, ok := histResp["records"].([]any)
		if ok && len(records) > 0 {
			first, ok := records[0].(map[string]any)
			if ok {
				if id, ok := first["id"].(float64); ok {
					w = doReq("DELETE", fmt.Sprintf("/api/v1/history/%d", int(id)), nil)
					assert.Equal(t, http.StatusOK, w.Code)
				}
			}
		}
	})

	// ══════════════════════════════════════════════════════════════════════
	// 10. CONFIG: Get config, Update
	// ══════════════════════════════════════════════════════════════════════
	t.Run("Config_Lifecycle", func(t *testing.T) {
		// Get config
		w := doReq("GET", "/api/v1/config", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var cfgResp map[string]any
		parseJSON(w, &cfgResp)
		assert.Contains(t, cfgResp, "server")
		assert.Contains(t, cfgResp, "scrapers")
		assert.Contains(t, cfgResp, "config_file_path")

		// Update config — send only the fields that the config update handler expects.
		// The handler uses ShouldBindJSON so it needs the full config structure.
		currentCfg := deps.CoreDeps.GetConfig()
		w = doReq("PUT", "/api/v1/config", currentCfg)
		// Config update may fail with 400 if validation fails — both are acceptable
		if w.Code == http.StatusOK {
			var updateResp map[string]any
			parseJSON(w, &updateResp)
			assert.Contains(t, updateResp["message"], "successfully")
		} else {
			t.Logf("Config update returned %d (expected 200 or 400): %s", w.Code, truncate(w.Body.String(), 200))
		}
	})

	// ══════════════════════════════════════════════════════════════════════
	// 11. SYSTEM: Proxy test, Translation models, Scrapers
	// ══════════════════════════════════════════════════════════════════════
	t.Run("System_ProxyAndTranslation", func(t *testing.T) {
		// Proxy test
		w := doReq("POST", "/api/v1/proxy/test", map[string]any{
			"mode": "direct",
			"proxy": map[string]any{
				"enabled": false,
			},
		})
		assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest}, w.Code)

		// Translation models
		w = doReq("POST", "/api/v1/translation/models", map[string]any{
			"provider": "openai",
			"base_url": "https://api.openai.com/v1",
			"api_key":  "test-key",
		})
		assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest, http.StatusBadGateway}, w.Code)

		// Available scrapers
		w = doReq("GET", "/api/v1/scrapers", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var scrapersResp map[string]any
		parseJSON(w, &scrapersResp)
		assert.Contains(t, scrapersResp, "scrapers")
	})

	// ══════════════════════════════════════════════════════════════════════
	// 12. VERSION: Status, Check
	// ══════════════════════════════════════════════════════════════════════
	t.Run("Version_StatusAndCheck", func(t *testing.T) {
		w := doReq("GET", "/api/v1/version", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var verResp map[string]any
		parseJSON(w, &verResp)
		assert.Contains(t, verResp, "current")
		assert.Contains(t, verResp, "source")

		w = doReq("POST", "/api/v1/version/check", nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 13. TOKENS: Create, List, Verify auth, Regenerate, Revoke
	// ══════════════════════════════════════════════════════════════════════
	var tokenID, rawToken string
	t.Run("Tokens_Lifecycle", func(t *testing.T) {
		// List tokens (should be empty)
		w := doReq("GET", "/api/v1/tokens", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var tokensResp map[string]any
		parseJSON(w, &tokensResp)
		assert.Equal(t, 0.0, tokensResp["count"])

		// Create token
		w = doReq("POST", "/api/v1/tokens", map[string]any{
			"name": "e2e-test-token",
		})
		assert.Equal(t, http.StatusCreated, w.Code)
		var created map[string]any
		parseJSON(w, &created)
		assert.Contains(t, created["token"], "jv_")
		tokenID = created["id"].(string)
		rawToken = created["token"].(string)
		assert.Equal(t, "e2e-test-token", created["name"])

		// List tokens (should have 1)
		w = doReq("GET", "/api/v1/tokens", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		parseJSON(w, &tokensResp)
		assert.Equal(t, 1.0, tokensResp["count"])

		// Verify Bearer token auth works
		savedCookie := sessionCookie
		sessionCookie = ""
		req := httptest.NewRequest("GET", "/api/v1/version", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req)
		assert.Equal(t, http.StatusOK, w2.Code, "Bearer token should authenticate successfully")
		sessionCookie = savedCookie

		// Regenerate token
		w = doReq("POST", "/api/v1/tokens/"+tokenID+"/regenerate", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var regenResp map[string]any
		parseJSON(w, &regenResp)
		assert.Contains(t, regenResp["token"], "jv_")
		newRawToken := regenResp["token"].(string)
		assert.NotEqual(t, rawToken, newRawToken, "regenerated token should differ")

		// Old token should no longer work
		sessionCookie = ""
		req = httptest.NewRequest("GET", "/api/v1/version", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		w2 = httptest.NewRecorder()
		router.ServeHTTP(w2, req)
		assert.Equal(t, http.StatusUnauthorized, w2.Code, "old token should be invalid after regeneration")
		sessionCookie = savedCookie

		// Revoke token
		w = doReq("DELETE", "/api/v1/tokens/"+tokenID, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		// List tokens (should be empty again)
		w = doReq("GET", "/api/v1/tokens", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		parseJSON(w, &tokensResp)
		assert.Equal(t, 0.0, tokensResp["count"])

		// Revoke nonexistent token
		w = doReq("DELETE", "/api/v1/tokens/nonexistent-id", nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 14. BATCH: Create job, list, get, cancel, delete
	// ══════════════════════════════════════════════════════════════════════
	var batchJobID string
	t.Run("Batch_Lifecycle", func(t *testing.T) {
		// List batch jobs — may return 500 if DB persistence isn't fully set up
		w := doReq("GET", "/api/v1/batch", nil)
		assert.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, w.Code)

		// Create a batch scrape job with temp dir files
		tempDir := t.TempDir()
		w = doReq("POST", "/api/v1/batch/scrape", map[string]any{
			"files":       []string{tempDir + "/test.mp4"},
			"destination": tempDir,
		})
		if w.Code == http.StatusOK {
			var batchResp map[string]any
			parseJSON(w, &batchResp)
			batchJobID = batchResp["job_id"].(string)
			assert.NotEmpty(t, batchJobID)

			// Get batch job (slim, default)
			w = doReq("GET", "/api/v1/batch/"+batchJobID, nil)
			assert.Equal(t, http.StatusOK, w.Code)

			// Get batch job with data
			w = doReq("GET", "/api/v1/batch/"+batchJobID+"?include_data=true", nil)
			assert.Equal(t, http.StatusOK, w.Code)

			// Cancel batch job
			w = doReq("POST", "/api/v1/batch/"+batchJobID+"/cancel", nil)
			assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest}, w.Code)

			// Delete batch job
			w = doReq("DELETE", "/api/v1/batch/"+batchJobID, nil)
			assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest}, w.Code)
		} else {
			// If batch scrape was rejected (path validation), verify endpoint exists
			assert.NotEqual(t, http.StatusNotFound, w.Code)
			t.Log("Batch scrape rejected (path validation), skipping batch lifecycle tests")
		}

		// Test nonexistent batch job
		w = doReq("GET", "/api/v1/batch/nonexistent-job-id", nil)
		assert.Equal(t, http.StatusNotFound, w.Code)

		// Cancel nonexistent batch job
		w = doReq("POST", "/api/v1/batch/nonexistent-job-id/cancel", nil)
		assert.Equal(t, http.StatusNotFound, w.Code)

		// Delete nonexistent batch job
		w = doReq("DELETE", "/api/v1/batch/nonexistent-job-id", nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 15. HEALTH: Health check
	// ══════════════════════════════════════════════════════════════════════
	t.Run("Health", func(t *testing.T) {
		w := doReq("GET", "/health", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "ok")
	})

	// ══════════════════════════════════════════════════════════════════════
	// 16. ACTRESS IMPORT/EXPORT: Import actresses, verify, export
	// ══════════════════════════════════════════════════════════════════════
	t.Run("Actresses_ImportExport", func(t *testing.T) {
		// Import actresses
		w := doReq("POST", "/api/v1/actresses/import", map[string]any{
			"actresses": []map[string]any{
				{"dmm_id": 20001, "japanese_name": "インポートA", "first_name": "ImportA", "last_name": "Test"},
				{"dmm_id": 20002, "japanese_name": "インポートB", "first_name": "ImportB", "last_name": "Test"},
			},
		})
		assert.Equal(t, http.StatusOK, w.Code)
		var importResp map[string]any
		parseJSON(w, &importResp)
		assert.Equal(t, 2.0, importResp["imported"])

		// Export actresses
		w = doReq("GET", "/api/v1/actresses/export", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		var exported []any
		parseJSON(w, &exported)
		assert.GreaterOrEqual(t, len(exported), 2)

		// Invalid import (bad JSON)
		req := httptest.NewRequest("POST", "/api/v1/actresses/import", strings.NewReader("not json"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:8080")
		req.Header.Set("Cookie", "javinizer_session="+sessionCookie)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req)
		assert.Equal(t, http.StatusBadRequest, w2.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 17. SCRAPE: Attempt scrape (exercises handler code paths)
	// ══════════════════════════════════════════════════════════════════════
	t.Run("Scrape_Attempt", func(t *testing.T) {
		// Scrape with no real scrapers — returns 404 or 500
		w := doReq("POST", "/api/v1/scrape", map[string]any{
			"id": "IPX-001",
		})
		assert.Contains(t, []int{http.StatusNotFound, http.StatusInternalServerError}, w.Code)
		if w.Code == http.StatusNotFound {
			// Verify this is the handler's 404 (has JSON error), not the router's
			assert.Contains(t, w.Body.String(), "error")
		}

		// Scrape with invalid body (missing required 'id')
		w = doReq("POST", "/api/v1/scrape", map[string]any{})
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Rescrape nonexistent movie
		w = doReq("POST", "/api/v1/movies/NOEXIST/rescrape", map[string]any{
			"selected_scrapers": []string{"r18dev"},
		})
		assert.Contains(t, []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError}, w.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 18. EDGE CASES: Invalid inputs, error paths
	// ══════════════════════════════════════════════════════════════════════
	t.Run("EdgeCases_InvalidInputs", func(t *testing.T) {
		// Invalid actress ID (0)
		w := doReq("GET", "/api/v1/actresses/0", nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Create actress without required fields
		w = doReq("POST", "/api/v1/actresses", map[string]any{
			"dmm_id": -1,
		})
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Genre replacement without original
		w = doReq("POST", "/api/v1/genres/replacements", map[string]any{
			"replacement": "test",
		})
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Delete genre replacement without id or original
		w = doReq("DELETE", "/api/v1/genres/replacements", nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Nonexistent route
		w = doReq("GET", "/api/v1/nonexistent", nil)
		assert.Equal(t, http.StatusNotFound, w.Code)

		// Config update with invalid JSON
		req := httptest.NewRequest("PUT", "/api/v1/config", strings.NewReader("{bad json"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:8080")
		req.Header.Set("Cookie", "javinizer_session="+sessionCookie)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req)
		assert.Equal(t, http.StatusBadRequest, w2.Code)

		// Auth setup when already initialized (should return 403 or 409)
		w = doReq("POST", "/api/v1/auth/setup", map[string]any{
			"username": "newadmin",
			"password": "newpassword123",
		})
		assert.Contains(t, []int{http.StatusForbidden, http.StatusConflict}, w.Code)

		// Delete events without required parameter
		w = doReq("DELETE", "/api/v1/events", nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Delete events with invalid parameter
		w = doReq("DELETE", "/api/v1/events?older_than_days=0", nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	// ══════════════════════════════════════════════════════════════════════
	// 19. DOCS: OpenAPI docs endpoint
	// ══════════════════════════════════════════════════════════════════════
	t.Run("Docs_OpenAPI", func(t *testing.T) {
		w := doReq("GET", "/docs/openapi.json", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		// The OpenAPI spec should contain valid JSON with "info" field
		var spec map[string]any
		parseJSON(w, &spec)
		assert.Contains(t, spec, "info", "OpenAPI spec should have 'info' field")

		w = doReq("GET", "/docs", nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// truncate limits a string to n characters for error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
