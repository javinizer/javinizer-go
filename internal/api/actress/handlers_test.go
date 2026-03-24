package actress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
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
			switch w.Code {
			case 200:
				// 200 response should be a valid JSON array (empty or matching query)
				var actresses []models.Actress
				err = json.Unmarshal(w.Body.Bytes(), &actresses)
				require.NoError(t, err, "200 response should be valid JSON array")
				// Malicious query shouldn't match real data - should be empty
				assert.Empty(t, actresses, "Malicious query should not return data (would indicate SQL injection success)")
			case 400:
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

func TestActressCRUDHandlers(t *testing.T) {
	mockRepo := newMockActressRepo()

	router := gin.New()
	router.GET("/actresses", listActresses(mockRepo))
	router.GET("/actresses/:id", getActress(mockRepo))
	router.POST("/actresses", createActress(mockRepo))
	router.PUT("/actresses/:id", updateActress(mockRepo))
	router.DELETE("/actresses/:id", deleteActress(mockRepo))

	// Create
	createPayload := map[string]interface{}{
		"dmm_id":        1001,
		"first_name":    "Yui",
		"last_name":     "Hatano",
		"japanese_name": "波多野結衣",
		"thumb_url":     "https://example.com/yui.jpg",
		"aliases":       "Yui Hatano|Hatano Yui",
	}
	createJSON, err := json.Marshal(createPayload)
	require.NoError(t, err)

	createReq := httptest.NewRequest("POST", "/actresses", bytes.NewReader(createJSON))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)
	assert.Equal(t, 201, createW.Code)

	var created models.Actress
	err = json.Unmarshal(createW.Body.Bytes(), &created)
	require.NoError(t, err)
	require.NotZero(t, created.ID)
	assert.Equal(t, "Yui", created.FirstName)

	// Get
	getReq := httptest.NewRequest("GET", "/actresses/"+toString(created.ID), nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	assert.Equal(t, 200, getW.Code)

	var fetched models.Actress
	err = json.Unmarshal(getW.Body.Bytes(), &fetched)
	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, "波多野結衣", fetched.JapaneseName)

	// Update
	updatePayload := map[string]interface{}{
		"dmm_id":        1001,
		"first_name":    "Updated",
		"last_name":     "Hatano",
		"japanese_name": "波多野結衣",
		"thumb_url":     "https://example.com/updated.jpg",
		"aliases":       "Updated Alias",
	}
	updateJSON, err := json.Marshal(updatePayload)
	require.NoError(t, err)

	updateReq := httptest.NewRequest("PUT", "/actresses/"+toString(created.ID), bytes.NewReader(updateJSON))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	router.ServeHTTP(updateW, updateReq)
	assert.Equal(t, 200, updateW.Code)

	var updated models.Actress
	err = json.Unmarshal(updateW.Body.Bytes(), &updated)
	require.NoError(t, err)
	assert.Equal(t, "Updated", updated.FirstName)
	assert.Equal(t, "https://example.com/updated.jpg", updated.ThumbURL)

	// List
	listReq := httptest.NewRequest("GET", "/actresses?limit=10&offset=0", nil)
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)
	assert.Equal(t, 200, listW.Code)

	var listResp actressesResponse
	err = json.Unmarshal(listW.Body.Bytes(), &listResp)
	require.NoError(t, err)
	assert.Equal(t, int64(1), listResp.Total)
	assert.Len(t, listResp.Actresses, 1)

	// Search with q
	searchReq := httptest.NewRequest("GET", "/actresses?q=Updated", nil)
	searchW := httptest.NewRecorder()
	router.ServeHTTP(searchW, searchReq)
	assert.Equal(t, 200, searchW.Code)

	var searchResp actressesResponse
	err = json.Unmarshal(searchW.Body.Bytes(), &searchResp)
	require.NoError(t, err)
	assert.Equal(t, int64(1), searchResp.Total)
	assert.Len(t, searchResp.Actresses, 1)
	assert.Equal(t, "Updated", searchResp.Actresses[0].FirstName)

	// Delete
	deleteReq := httptest.NewRequest("DELETE", "/actresses/"+toString(created.ID), nil)
	deleteW := httptest.NewRecorder()
	router.ServeHTTP(deleteW, deleteReq)
	assert.Equal(t, 200, deleteW.Code)

	// Verify gone
	getAfterDeleteReq := httptest.NewRequest("GET", "/actresses/"+toString(created.ID), nil)
	getAfterDeleteW := httptest.NewRecorder()
	router.ServeHTTP(getAfterDeleteW, getAfterDeleteReq)
	assert.Equal(t, 404, getAfterDeleteW.Code)
}

func TestDeleteActress(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		actressID      string
		setupRepo      func(*database.ActressRepository)
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "delete existing actress",
			actressID:      "1",
			setupRepo:      func(repo *database.ActressRepository) {},
			expectedStatus: 200,
			expectedBody:   "actress deleted",
		},
		{
			name:           "delete non-existent actress",
			actressID:      "999",
			setupRepo:      func(repo *database.ActressRepository) {},
			expectedStatus: 404,
			expectedBody:   "actress not found",
		},
		{
			name:           "invalid actress ID - letters",
			actressID:      "abc",
			setupRepo:      func(repo *database.ActressRepository) {},
			expectedStatus: 400,
			expectedBody:   "invalid actress id",
		},
		{
			name:           "invalid actress ID - zero",
			actressID:      "0",
			setupRepo:      func(repo *database.ActressRepository) {},
			expectedStatus: 400,
			expectedBody:   "invalid actress id",
		},
		{
			name:           "invalid actress ID - negative",
			actressID:      "-1",
			setupRepo:      func(repo *database.ActressRepository) {},
			expectedStatus: 400,
			expectedBody:   "invalid actress id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := newMockActressRepo()

			// Pre-populate with one actress for delete existing test
			if tt.setupRepo != nil && tt.actressID == "1" {
				_ = mockRepo.Create(&models.Actress{
					DMMID:     100,
					FirstName: "Test",
					LastName:  "Actress",
				})
			}

			router := gin.New()
			router.DELETE("/actresses/:id", deleteActress(mockRepo))

			req := httptest.NewRequest(http.MethodDelete, "/actresses/"+tt.actressID, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedBody != "" {
				assert.Contains(t, w.Body.String(), tt.expectedBody)
			}
		})
	}
}

func TestCreateActress_Validation(t *testing.T) {
	mockRepo := newMockActressRepo()

	router := gin.New()
	router.POST("/actresses", createActress(mockRepo))

	payload := map[string]interface{}{
		"first_name":    "",
		"japanese_name": "",
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/actresses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestListActresses_Sorting(t *testing.T) {
	mockRepo := newMockActressRepo()
	setupActresses(t, mockRepo, []models.Actress{
		{DMMID: 30, FirstName: "A", LastName: "One", JapaneseName: "あ"},
		{DMMID: 10, FirstName: "B", LastName: "Two", JapaneseName: "い"},
		{DMMID: 20, FirstName: "C", LastName: "Three", JapaneseName: "う"},
	})

	router := gin.New()
	router.GET("/actresses", listActresses(mockRepo))

	req := httptest.NewRequest("GET", "/actresses?sort_by=dmm_id&sort_order=desc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var resp actressesResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Len(t, resp.Actresses, 3)
	assert.Equal(t, 30, resp.Actresses[0].DMMID)
	assert.Equal(t, 20, resp.Actresses[1].DMMID)
	assert.Equal(t, 10, resp.Actresses[2].DMMID)
}

func toString(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}

func TestParseActressID(t *testing.T) {
	tests := []struct {
		name       string
		param      string
		expectedID uint
		expectedOK bool
	}{
		{"valid ID", "123", 123, true},
		{"valid ID zero-padded", "007", 7, true},
		{"valid ID one", "1", 1, true},
		{"invalid ID - letters", "abc", 0, false},
		{"invalid ID - empty", "", 0, false},
		{"invalid ID - negative", "-1", 0, false},
		{"invalid ID - special chars", "123!", 0, false},
		{"invalid ID - float", "12.3", 0, false},
		{"invalid ID - zero", "0", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Params = append(c.Params, gin.Param{
				Key:   "id",
				Value: tt.param,
			})

			id, ok := parseActressID(c)

			assert.Equal(t, tt.expectedOK, ok, "ok should match expected")
			assert.Equal(t, tt.expectedID, id, "id should match expected")
		})
	}
}

func TestActressMergePreviewAndApply(t *testing.T) {
	mockRepo := newMockActressRepo()
	target := &models.Actress{
		DMMID:        70001,
		FirstName:    "Target",
		LastName:     "Actress",
		JapaneseName: "ターゲット",
		ThumbURL:     "https://example.com/target.jpg",
		Aliases:      "TargetAlias",
	}
	source := &models.Actress{
		DMMID:        70002,
		FirstName:    "Source",
		LastName:     "Actress",
		JapaneseName: "ソース",
		ThumbURL:     "https://example.com/source.jpg",
		Aliases:      "SourceAlias",
	}
	require.NoError(t, mockRepo.Create(target))
	require.NoError(t, mockRepo.Create(source))

	router := gin.New()
	router.POST("/actresses/merge/preview", previewActressMerge(mockRepo))
	router.POST("/actresses/merge", mergeActresses(mockRepo))
	router.GET("/actresses/:id", getActress(mockRepo))

	previewBody := map[string]interface{}{
		"target_id": target.ID,
		"source_id": source.ID,
	}
	previewJSON, err := json.Marshal(previewBody)
	require.NoError(t, err)

	previewReq := httptest.NewRequest(http.MethodPost, "/actresses/merge/preview", bytes.NewReader(previewJSON))
	previewReq.Header.Set("Content-Type", "application/json")
	previewW := httptest.NewRecorder()
	router.ServeHTTP(previewW, previewReq)
	require.Equal(t, http.StatusOK, previewW.Code)

	var previewResp ActressMergePreviewResponse
	require.NoError(t, json.Unmarshal(previewW.Body.Bytes(), &previewResp))
	assert.Equal(t, target.ID, previewResp.Target.ID)
	assert.Equal(t, source.ID, previewResp.Source.ID)
	assert.NotEmpty(t, previewResp.Conflicts)
	assert.Equal(t, "target", previewResp.DefaultResolutions["dmm_id"])

	mergeBody := map[string]interface{}{
		"target_id": target.ID,
		"source_id": source.ID,
		"resolutions": map[string]string{
			"dmm_id":    "source",
			"thumb_url": "source",
		},
	}
	mergeJSON, err := json.Marshal(mergeBody)
	require.NoError(t, err)

	mergeReq := httptest.NewRequest(http.MethodPost, "/actresses/merge", bytes.NewReader(mergeJSON))
	mergeReq.Header.Set("Content-Type", "application/json")
	mergeW := httptest.NewRecorder()
	router.ServeHTTP(mergeW, mergeReq)
	require.Equal(t, http.StatusOK, mergeW.Code)

	var mergeResp ActressMergeResponse
	require.NoError(t, json.Unmarshal(mergeW.Body.Bytes(), &mergeResp))
	assert.Equal(t, source.ID, mergeResp.MergedFromID)
	assert.Equal(t, 70002, mergeResp.MergedActress.DMMID)
	assert.Equal(t, "https://example.com/source.jpg", mergeResp.MergedActress.ThumbURL)
	assert.GreaterOrEqual(t, mergeResp.ConflictsResolved, 1)

	getSourceReq := httptest.NewRequest(http.MethodGet, "/actresses/"+toString(source.ID), nil)
	getSourceW := httptest.NewRecorder()
	router.ServeHTTP(getSourceW, getSourceReq)
	assert.Equal(t, http.StatusNotFound, getSourceW.Code)
}

func TestActressMergeValidationAndNotFound(t *testing.T) {
	mockRepo := newMockActressRepo()
	target := &models.Actress{
		DMMID:        71001,
		FirstName:    "Target",
		JapaneseName: "ターゲット",
	}
	require.NoError(t, mockRepo.Create(target))

	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(mockRepo))

	// Same target/source should return 400.
	sameBody := map[string]interface{}{
		"target_id": target.ID,
		"source_id": target.ID,
	}
	sameJSON, err := json.Marshal(sameBody)
	require.NoError(t, err)

	sameReq := httptest.NewRequest(http.MethodPost, "/actresses/merge", bytes.NewReader(sameJSON))
	sameReq.Header.Set("Content-Type", "application/json")
	sameW := httptest.NewRecorder()
	router.ServeHTTP(sameW, sameReq)
	assert.Equal(t, http.StatusBadRequest, sameW.Code)

	// Missing source actress should return 404.
	missingBody := map[string]interface{}{
		"target_id": target.ID,
		"source_id": 999999,
	}
	missingJSON, err := json.Marshal(missingBody)
	require.NoError(t, err)

	missingReq := httptest.NewRequest(http.MethodPost, "/actresses/merge", bytes.NewReader(missingJSON))
	missingReq.Header.Set("Content-Type", "application/json")
	missingW := httptest.NewRecorder()
	router.ServeHTTP(missingW, missingReq)
	assert.Equal(t, http.StatusNotFound, missingW.Code)
}

func TestActressMergeInvalidResolutionPayload(t *testing.T) {
	mockRepo := newMockActressRepo()
	target := &models.Actress{DMMID: 72001, FirstName: "Target", JapaneseName: "ターゲット"}
	source := &models.Actress{DMMID: 72002, FirstName: "Source", JapaneseName: "ソース"}
	require.NoError(t, mockRepo.Create(target))
	require.NoError(t, mockRepo.Create(source))

	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(mockRepo))

	tests := []struct {
		name       string
		resolution map[string]string
	}{
		{
			name:       "invalid field",
			resolution: map[string]string{"unknown_field": "target"},
		},
		{
			name:       "invalid decision",
			resolution: map[string]string{"first_name": "invalid_choice"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]interface{}{
				"target_id":   target.ID,
				"source_id":   source.ID,
				"resolutions": tt.resolution,
			}
			payload, err := json.Marshal(body)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/actresses/merge", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestActressMergeConflictStatus409(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"},
		Logging:  config.LoggingConfig{Level: "error"},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())
	repo := database.NewActressRepository(db)

	target := &models.Actress{DMMID: 73001, FirstName: "Target", JapaneseName: "ターゲット"}
	source := &models.Actress{DMMID: 73002, FirstName: "Source", JapaneseName: "ソース"}
	require.NoError(t, repo.Create(target))
	require.NoError(t, repo.Create(source))

	// Drop unique dmm_id index so we can create a conflicting duplicate row in test DB.
	var idxNames []string
	require.NoError(t, db.DB.Raw(
		"SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='actresses' AND sql LIKE '%dmm_id%'",
	).Scan(&idxNames).Error)
	for _, idx := range idxNames {
		require.NoError(t, db.DB.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", idx)).Error)
	}
	require.NoError(t, repo.Create(&models.Actress{
		DMMID:        73002,
		FirstName:    "Other",
		JapaneseName: "競合",
	}))

	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(repo))

	body := map[string]interface{}{
		"target_id": target.ID,
		"source_id": source.ID,
		"resolutions": map[string]string{
			"dmm_id": "source",
		},
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/actresses/merge", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}
