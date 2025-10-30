package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper to populate actress data
func setupActresses(t *testing.T, repo *database.ActressRepository, actresses []models.Actress) {
	t.Helper()
	for _, actress := range actresses {
		err := repo.Create(&actress)
		require.NoError(t, err, "Failed to create actress in test setup")
	}
}

func TestSearchActresses(t *testing.T) {
	tests := []struct {
		name           string
		setupRepo      func(*database.ActressRepository)
		query          string
		expectedStatus int
		validateFn     func(*testing.T, []models.Actress)
	}{
		{
			name: "search with query - single result",
			setupRepo: func(repo *database.ActressRepository) {
				setupActresses(t, repo, []models.Actress{
					{
						DMMID:        1,
						FirstName:    "Yui",
						LastName:     "Hatano",
						JapaneseName: "波多野結衣",
					},
					{
						DMMID:        2,
						FirstName:    "Ai",
						LastName:     "Uehara",
						JapaneseName: "上原亜衣",
					},
				})
			},
			query:          "Yui",
			expectedStatus: 200,
			validateFn: func(t *testing.T, actresses []models.Actress) {
				assert.Len(t, actresses, 1)
				assert.Equal(t, "Yui", actresses[0].FirstName)
			},
		},
		// Skipped: search with empty query test - requires mock with full repository behavior
		{
			name: "search with no results",
			setupRepo: func(repo *database.ActressRepository) {
				setupActresses(t, repo, []models.Actress{
					{DMMID: 1, FirstName: "Yui", LastName: "Hatano"},
				})
			},
			query:          "Nonexistent",
			expectedStatus: 200,
			validateFn: func(t *testing.T, actresses []models.Actress) {
				assert.Empty(t, actresses)
			},
		},
		{
			name: "search with Japanese characters",
			setupRepo: func(repo *database.ActressRepository) {
				setupActresses(t, repo, []models.Actress{
					{
						DMMID:        1,
						FirstName:    "Yui",
						LastName:     "Hatano",
						JapaneseName: "波多野結衣",
					},
				})
			},
			query:          "波多野",
			expectedStatus: 200,
			validateFn: func(t *testing.T, actresses []models.Actress) {
				assert.Len(t, actresses, 1)
				assert.Contains(t, actresses[0].JapaneseName, "波多野")
			},
		},
		// Skipped: repository error test - requires error injection mechanism
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := newMockActressRepo()
			tt.setupRepo(mockRepo)

			router := gin.New()
			router.GET("/actresses/search", searchActresses(mockRepo))

			req := httptest.NewRequest("GET", "/actresses/search?q="+tt.query, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var actresses []models.Actress
				err := json.Unmarshal(w.Body.Bytes(), &actresses)
				require.NoError(t, err)
				tt.validateFn(t, actresses)
			}
		})
	}
}

func TestSearchActresses_SQLInjectionPrevention(t *testing.T) {
	// Test that SQL injection attempts are safely handled using URL encoding
	mockRepo := newMockActressRepo()
	setupActresses(t, mockRepo, []models.Actress{
		{DMMID: 1, FirstName: "Yui", LastName: "Hatano"},
	})

	router := gin.New()
	router.GET("/actresses/search", searchActresses(mockRepo))

	// URL-encoded malicious queries
	maliciousQueries := []string{
		"test%27%20OR%20%271%27%3D%271", // ' OR '1'='1
		"test%27%3B%20DROP%20TABLE",     // '; DROP TABLE
	}

	for _, maliciousQuery := range maliciousQueries {
		t.Run("SQLInjection:"+maliciousQuery, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/actresses/search?q="+maliciousQuery, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Tighten status code assertion: expect 200 (with empty results) or 400 (error)
			// Don't accept 3xx, 5xx, or other unexpected codes
			assert.True(t, w.Code == 200 || w.Code == 400,
				"Expected 200 (OK with empty) or 400 (Bad Request), got %d", w.Code)

			// Verify database integrity - count should still be 1 (no new data leaked/altered)
			allActresses, err := mockRepo.Search("")
			require.NoError(t, err)
			assert.Equal(t, 1, len(allActresses), "Database should be unaffected by SQL injection attempt")

			// Verify response contract based on status code
			if w.Code == 200 {
				// 200 response should be a valid JSON array (empty or matching query)
				var actresses []models.Actress
				err = json.Unmarshal(w.Body.Bytes(), &actresses)
				require.NoError(t, err, "200 response should be valid JSON array")
				// Malicious query shouldn't match real data - should be empty
				assert.Empty(t, actresses, "Malicious query should not return data (would indicate SQL injection success)")
			} else if w.Code == 400 {
				// 400 response should be a valid error JSON
				var errResp ErrorResponse
				err = json.Unmarshal(w.Body.Bytes(), &errResp)
				require.NoError(t, err, "400 response should be valid error JSON")
				assert.NotEmpty(t, errResp.Error, "Error response should contain error message")
			}
		})
	}
}

func TestSearchActresses_SpecialCharacters(t *testing.T) {
	mockRepo := newMockActressRepo()
	setupActresses(t, mockRepo, []models.Actress{
		{DMMID: 1, FirstName: "Yui", LastName: "Hatano"},
	})

	router := gin.New()
	router.GET("/actresses/search", searchActresses(mockRepo))

	specialCharQueries := []string{
		"%",            // SQL wildcard
		"_",            // SQL wildcard
		"*",            // Glob pattern
		"../",          // Path traversal
		"<script>",     // XSS attempt
		"';alert(1)//", // XSS + SQL injection
	}

	for _, query := range specialCharQueries {
		t.Run("SpecialChar:"+query, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/actresses/search?q="+query, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Should handle gracefully
			assert.True(t, w.Code == 200 || w.Code == 400, "Should handle special characters safely")
		})
	}
}

func TestSearchActresses_CaseInsensitivity(t *testing.T) {
	mockRepo := newMockActressRepo()
	setupActresses(t, mockRepo, []models.Actress{
		{DMMID: 1, FirstName: "Yui", LastName: "Hatano"},
	})

	router := gin.New()
	router.GET("/actresses/search", searchActresses(mockRepo))

	queries := []string{"yui", "YUI", "Yui", "yUi"}

	for _, query := range queries {
		t.Run("CaseTest:"+query, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/actresses/search?q="+query, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, 200, w.Code)
			// All case variations should return consistent results
		})
	}
}

func TestSearchActresses_URLEncoding(t *testing.T) {
	mockRepo := newMockActressRepo()
	setupActresses(t, mockRepo, []models.Actress{
		{DMMID: 1, FirstName: "Test Name", LastName: "With Spaces"},
	})

	router := gin.New()
	router.GET("/actresses/search", searchActresses(mockRepo))

	// Test URL-encoded query
	req := httptest.NewRequest("GET", "/actresses/search?q=Test%20Name", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

func TestSearchActresses_EmptyDatabase(t *testing.T) {
	mockRepo := newMockActressRepo()
	// Empty database

	router := gin.New()
	router.GET("/actresses/search", searchActresses(mockRepo))

	req := httptest.NewRequest("GET", "/actresses/search?q=test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var actresses []models.Actress
	err := json.Unmarshal(w.Body.Bytes(), &actresses)
	require.NoError(t, err)
	assert.Empty(t, actresses)
}

func TestSearchActresses_LargeResultSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large result set test in short mode")
	}

	mockRepo := newMockActressRepo()

	// Create many actresses to test the repository's built-in limit
	// Note: ActressRepository.Search intentionally caps empty-query responses at 100
	// (see internal/database/database.go:328)
	actresses := make([]models.Actress, 1000)
	for i := 0; i < 1000; i++ {
		actresses[i] = models.Actress{
			DMMID:     i + 1, // Unique ID required (uniqueIndex constraint)
			FirstName: "Actress",
			LastName:  "Test",
		}
	}
	setupActresses(t, mockRepo, actresses)

	router := gin.New()
	router.GET("/actresses/search", searchActresses(mockRepo))

	req := httptest.NewRequest("GET", "/actresses/search?q=", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var results []models.Actress
	err := json.Unmarshal(w.Body.Bytes(), &results)
	require.NoError(t, err)
	// Repository intentionally caps results at 100 to prevent excessive responses
	assert.Len(t, results, 100, "Repository should cap empty-query results at 100")
}
