package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanDirectory(t *testing.T) {
	tests := []struct {
		name           string
		setupFiles     func(*testing.T, string) string // Returns path to scan
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *ScanResponse)
	}{
		{
			name: "scan directory with video files",
			setupFiles: func(t *testing.T, tempDir string) string {
				// Create test video files
				testFile1 := filepath.Join(tempDir, "IPX-535.mp4")
				testFile2 := filepath.Join(tempDir, "ABC-123.mkv")
				require.NoError(t, os.WriteFile(testFile1, []byte("test"), 0644))
				require.NoError(t, os.WriteFile(testFile2, []byte("test"), 0644))
				return tempDir
			},
			requestBody: func(path string) ScanRequest {
				return ScanRequest{
					Path:      path,
					Recursive: false,
				}
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *ScanResponse) {
				assert.Greater(t, resp.Count, 0)
				assert.NotEmpty(t, resp.Files)
				// Should match video files
				matchedCount := 0
				for _, file := range resp.Files {
					if file.Matched {
						matchedCount++
					}
				}
				assert.Greater(t, matchedCount, 0, "Should match at least one video file")
			},
		},
		{
			name: "scan directory with date-based uncensored filenames",
			setupFiles: func(t *testing.T, tempDir string) string {
				testFiles := []string{
					"020326_001-1PON.mp4",
					"020326_01-10MU.mp4",
					"123025-001-CARIB.mp4",
				}
				for _, file := range testFiles {
					path := filepath.Join(tempDir, file)
					require.NoError(t, os.WriteFile(path, []byte("test"), 0644))
				}
				return tempDir
			},
			requestBody: func(path string) ScanRequest {
				return ScanRequest{
					Path:      path,
					Recursive: false,
				}
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *ScanResponse) {
				matchedByName := map[string]bool{}
				for _, file := range resp.Files {
					if file.Matched {
						matchedByName[file.Name] = true
					}
				}
				assert.True(t, matchedByName["020326_001-1PON.mp4"])
				assert.True(t, matchedByName["020326_01-10MU.mp4"])
				assert.True(t, matchedByName["123025-001-CARIB.mp4"])
			},
		},
		{
			name: "scan non-existent directory",
			setupFiles: func(_ *testing.T, tempDir string) string {
				return filepath.Join(tempDir, "nonexistent")
			},
			requestBody: func(path string) ScanRequest {
				return ScanRequest{
					Path: path,
				}
			},
			expectedStatus: 400,
		},
		{
			name: "scan directory with non-video files",
			setupFiles: func(t *testing.T, tempDir string) string {
				// Create non-video files
				testFile := filepath.Join(tempDir, "document.txt")
				require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))
				return tempDir
			},
			requestBody: func(path string) ScanRequest {
				return ScanRequest{
					Path: path,
				}
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *ScanResponse) {
				// Should complete but may skip non-video files
				assert.NotNil(t, resp.Files)
			},
		},
		{
			name: "invalid request - missing path",
			setupFiles: func(_ *testing.T, tempDir string) string {
				return tempDir
			},
			requestBody:    map[string]string{},
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			scanPath := tt.setupFiles(t, tempDir)

			cfg := &config.Config{
				Matching: config.MatchingConfig{
					RegexEnabled: false,
					Extensions:   []string{".mp4", ".mkv", ".avi"},
				},
			}

			mat, err := matcher.NewMatcher(&cfg.Matching)
			require.NoError(t, err)

			// Create minimal ServerDependencies for test
			deps := &ServerDependencies{
				Matcher: mat,
			}
			deps.SetConfig(cfg)

			router := gin.New()
			router.POST("/scan", scanDirectory(deps))

			var reqBody interface{}
			if fn, ok := tt.requestBody.(func(string) ScanRequest); ok {
				reqBody = fn(scanPath)
			} else {
				reqBody = tt.requestBody
			}

			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/scan", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response ScanResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestScanDirectory_PathTraversalPrevention(t *testing.T) {
	// Test that path traversal attempts and system directory access are properly blocked
	tempDir := t.TempDir()

	cfg := &config.Config{
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
	}

	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err)

	// Create minimal ServerDependencies for test
	deps := &ServerDependencies{
		Matcher: mat,
	}
	deps.SetConfig(cfg)

	router := gin.New()
	router.POST("/scan", scanDirectory(deps))

	tests := []struct {
		name             string
		path             string
		expectedStatus   int      // Single expected status (use 0 if acceptedStatuses is set)
		acceptedStatuses []int    // Multiple acceptable statuses (for platform-dependent behavior)
		errorContains    string   // Single error substring (use "" if acceptedErrors is set)
		acceptedErrors   []string // Multiple acceptable error substrings
	}{
		{
			name:           "valid temp directory",
			path:           tempDir,
			expectedStatus: 200,
		},
		{
			name:           "system directory /etc - should block",
			path:           "/etc",
			expectedStatus: 403,
			errorContains:  "system directory",
		},
		{
			name: "path traversal with ../ - should block",
			path: filepath.Join(tempDir, "../../../etc"),
			// Accept either 400 (path doesn't exist) or 403 (system directory blocked)
			// Behavior depends on temp directory depth:
			// - macOS: /var/folders/.../T/test/../../../etc → /var/folders/.../etc (doesn't exist) → 400
			// - Linux: /tmp/test/../../../etc → /etc (exists, blocked) → 403
			acceptedStatuses: []int{400, 403},
			acceptedErrors:   []string{"does not exist", "system directory", "access denied"},
		},
		{
			name:           "nonexistent path",
			path:           "/nonexistent/path/12345",
			expectedStatus: 400,
			errorContains:  "does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := ScanRequest{Path: tt.path}
			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/scan", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Handle status code validation
			if len(tt.acceptedStatuses) > 0 {
				// Multiple acceptable statuses (platform-dependent behavior)
				statusMatched := false
				for _, acceptedStatus := range tt.acceptedStatuses {
					if w.Code == acceptedStatus {
						statusMatched = true
						break
					}
				}
				assert.True(t, statusMatched,
					"Expected one of %v, got %d. Response: %s", tt.acceptedStatuses, w.Code, w.Body.String())
			} else {
				// Single expected status
				assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())
			}

			// Handle error message validation
			if len(tt.acceptedErrors) > 0 {
				// Multiple acceptable error messages
				responseBody := w.Body.String()
				errorMatched := false
				for _, acceptedError := range tt.acceptedErrors {
					if strings.Contains(responseBody, acceptedError) {
						errorMatched = true
						break
					}
				}
				assert.True(t, errorMatched,
					"Expected response to contain one of %v, got: %s", tt.acceptedErrors, responseBody)
			} else if tt.errorContains != "" {
				// Single expected error substring
				assert.Contains(t, w.Body.String(), tt.errorContains)
			}
		})
	}
}

func TestGetCurrentWorkingDirectory(t *testing.T) {
	t.Run("returns os.Getwd when no allowed directories configured", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.API.Security.AllowedDirectories = []string{} // No allowed directories

		// Create minimal ServerDependencies for test
		deps := &ServerDependencies{}
		deps.SetConfig(cfg)

		router := gin.New()
		router.GET("/cwd", getCurrentWorkingDirectory(deps))

		req := httptest.NewRequest("GET", "/cwd", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)

		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Contains(t, response, "path")
		assert.NotEmpty(t, response["path"])

		// Should return actual working directory
		expectedCwd, _ := os.Getwd()
		assert.Equal(t, expectedCwd, response["path"])
	})

	t.Run("returns first allowed directory when configured", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.API.Security.AllowedDirectories = []string{"/media", "/data"}

		// Create minimal ServerDependencies for test
		deps := &ServerDependencies{}
		deps.SetConfig(cfg)

		router := gin.New()
		router.GET("/cwd", getCurrentWorkingDirectory(deps))

		req := httptest.NewRequest("GET", "/cwd", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)

		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Contains(t, response, "path")
		// Should return first allowed directory
		assert.Equal(t, "/media", response["path"])
	})
}

func TestBrowseDirectory(t *testing.T) {
	tests := []struct {
		name           string
		setupFiles     func(*testing.T, string) string // Returns path to browse
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *BrowseResponse)
	}{
		{
			name: "browse directory successfully",
			setupFiles: func(t *testing.T, tempDir string) string {
				// Create test files and subdirectories
				require.NoError(t, os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("test"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(tempDir, "file2.mp4"), []byte("test"), 0644))
				require.NoError(t, os.Mkdir(filepath.Join(tempDir, "subdir"), 0755))
				return tempDir
			},
			requestBody: func(path string) BrowseRequest {
				return BrowseRequest{Path: path}
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BrowseResponse) {
				assert.NotEmpty(t, resp.CurrentPath)
				assert.NotEmpty(t, resp.Items)
				assert.GreaterOrEqual(t, len(resp.Items), 3) // file1, file2, subdir

				// Verify files have proper metadata
				for _, item := range resp.Items {
					assert.NotEmpty(t, item.Name)
					assert.NotEmpty(t, item.Path)
					assert.NotEmpty(t, item.ModTime)
				}

				// Check for subdirectory
				hasDir := false
				for _, item := range resp.Items {
					if item.IsDir {
						hasDir = true
						break
					}
				}
				assert.True(t, hasDir, "Should identify subdirectory")
			},
		},
		{
			name: "browse non-existent directory",
			setupFiles: func(_ *testing.T, tempDir string) string {
				return filepath.Join(tempDir, "nonexistent")
			},
			requestBody: func(path string) BrowseRequest {
				return BrowseRequest{Path: path}
			},
			expectedStatus: 400,
		},
		{
			name: "browse file instead of directory",
			setupFiles: func(t *testing.T, tempDir string) string {
				filePath := filepath.Join(tempDir, "file.txt")
				require.NoError(t, os.WriteFile(filePath, []byte("test"), 0644))
				return filePath
			},
			requestBody: func(path string) BrowseRequest {
				return BrowseRequest{Path: path}
			},
			expectedStatus: 400,
		},
		{
			name: "browse with empty path defaults to cwd",
			setupFiles: func(_ *testing.T, tempDir string) string {
				return ""
			},
			requestBody:    BrowseRequest{Path: ""},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BrowseResponse) {
				assert.NotEmpty(t, resp.CurrentPath)
			},
		},
		{
			name: "invalid JSON",
			setupFiles: func(_ *testing.T, tempDir string) string {
				return tempDir
			},
			requestBody:    "invalid json",
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			browsePath := tt.setupFiles(t, tempDir)

			cfg := config.DefaultConfig()

			// Create minimal ServerDependencies for test
			deps := &ServerDependencies{}
			deps.SetConfig(cfg)

			router := gin.New()
			router.POST("/browse", browseDirectory(deps))

			var reqBody interface{}
			if fn, ok := tt.requestBody.(func(string) BrowseRequest); ok {
				reqBody = fn(browsePath)
			} else {
				reqBody = tt.requestBody
			}

			var body []byte
			var err error
			if str, ok := reqBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(reqBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/browse", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response BrowseResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestBrowseDirectory_PathTraversalPrevention(t *testing.T) {
	// CRITICAL: Create temp directory and configure allowlist
	// DefaultConfig() has empty AllowedDirectories, which allows traversal to parent directories
	// This test must verify that paths outside the allowlist are rejected
	tempDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.API.Security.AllowedDirectories = []string{tempDir} // Only allow tempDir

	// Create minimal ServerDependencies for test
	deps := &ServerDependencies{}
	deps.SetConfig(cfg)

	router := gin.New()
	router.POST("/browse", browseDirectory(deps))

	maliciousPaths := []string{
		// Relative traversal attempts (from tempDir context)
		filepath.Join(tempDir, "../../../etc"),                           // Try to escape to /etc
		filepath.Join(tempDir, "..\\..\\..\\windows"),                    // Windows-style escape
		filepath.Join(tempDir, "..", "..", ".."),                         // Multiple parent dirs
		filepath.Join(tempDir, "..", filepath.Base(tempDir), "..", ".."), // Navigate back then escape

		// Absolute paths outside allowlist
		"/etc",        // Unix system directory
		"/tmp",        // Different directory
		"C:\\Windows", // Windows system directory
		"/Users",      // Common parent directory
	}

	for _, maliciousPath := range maliciousPaths {
		t.Run("PathTraversal:"+maliciousPath, func(t *testing.T) {
			reqBody := BrowseRequest{Path: maliciousPath}
			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/browse", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Path traversal should be explicitly denied with 403 or 400, not just "not 500"
			// This prevents regression where handler returns 200 with sensitive data
			assert.True(t, w.Code == 403 || w.Code == 400,
				"Expected 403 (Forbidden) or 400 (Bad Request) for path traversal attempt, got %d", w.Code)

			// Verify error response contains appropriate message
			var response ErrorResponse
			err = json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err, "Response should be valid JSON error")
			assert.NotEmpty(t, response.Error, "Error message should be present")

			// Accept various security-related error messages (access denied, path not exist, etc.)
			errorLower := strings.ToLower(response.Error)
			hasSecurityError := strings.Contains(errorLower, "access denied") ||
				strings.Contains(errorLower, "path does not exist") ||
				strings.Contains(errorLower, "forbidden") ||
				strings.Contains(errorLower, "not allowed")
			assert.True(t, hasSecurityError,
				"Error message should indicate security rejection for path: %s, got: %s", maliciousPath, response.Error)
		})
	}
}

func TestBrowseDirectory_ParentPathCalculation(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	cfg := config.DefaultConfig()
	// Create minimal ServerDependencies for test
	deps := &ServerDependencies{}
	deps.SetConfig(cfg)

	router := gin.New()
	router.POST("/browse", browseDirectory(deps))

	// Browse subdirectory
	reqBody := BrowseRequest{Path: subDir}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/browse", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response BrowseResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	// Parent path should be correctly calculated
	assert.Equal(t, subDir, response.CurrentPath)
	assert.Equal(t, tempDir, response.ParentPath)
}

func TestAutocompletePath(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tempDir, "archive"), 0755))
	require.NoError(t, os.Mkdir(filepath.Join(tempDir, "unsorted"), 0755))
	require.NoError(t, os.Mkdir(filepath.Join(tempDir, "unsorted-4k"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "unsorted.txt"), []byte("test"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(tempDir, "unsorted", "incoming"), 0755))
	require.NoError(t, os.Mkdir(filepath.Join(tempDir, "unsorted", "processed"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "unsorted", "video.mp4"), []byte("test"), 0644))

	cfg := config.DefaultConfig()
	cfg.API.Security.AllowedDirectories = []string{tempDir}
	canonicalTempDir, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err)

	deps := &ServerDependencies{}
	deps.SetConfig(cfg)

	router := gin.New()
	router.POST("/browse/autocomplete", autocompletePath(deps))

	t.Run("matches partial final segment", func(t *testing.T) {
		reqBody := PathAutocompleteRequest{
			Path:  filepath.Join(tempDir, "uns"),
			Limit: 10,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/browse/autocomplete", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.Equal(t, 200, w.Code, w.Body.String())

		var response PathAutocompleteResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, canonicalTempDir, response.BasePath)
		require.Len(t, response.Suggestions, 2)
		assert.Equal(t, "unsorted", response.Suggestions[0].Name)
		assert.Equal(t, filepath.Join(canonicalTempDir, "unsorted"), response.Suggestions[0].Path)
		assert.Equal(t, "unsorted-4k", response.Suggestions[1].Name)
		assert.Equal(t, filepath.Join(canonicalTempDir, "unsorted-4k"), response.Suggestions[1].Path)
	})

	t.Run("trailing separator lists child directories only", func(t *testing.T) {
		reqBody := PathAutocompleteRequest{
			Path: filepath.Join(tempDir, "unsorted") + string(os.PathSeparator),
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/browse/autocomplete", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.Equal(t, 200, w.Code, w.Body.String())

		var response PathAutocompleteResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, filepath.Join(canonicalTempDir, "unsorted"), response.BasePath)
		require.Len(t, response.Suggestions, 2)
		assert.Equal(t, "incoming", response.Suggestions[0].Name)
		assert.Equal(t, "processed", response.Suggestions[1].Name)
	})
}

func TestAutocompletePath_PathTraversalPrevention(t *testing.T) {
	tempDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.API.Security.AllowedDirectories = []string{tempDir}

	deps := &ServerDependencies{}
	deps.SetConfig(cfg)

	router := gin.New()
	router.POST("/browse/autocomplete", autocompletePath(deps))

	reqBody := PathAutocompleteRequest{
		Path: filepath.Join(tempDir, "..", "etc"),
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/browse/autocomplete", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.True(t, w.Code == 400 || w.Code == 403, "expected security rejection, got %d", w.Code)

	var response ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.NotEmpty(t, response.Error)
}

func TestScanDirectory_LargeDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large directory test in short mode")
	}

	tempDir := t.TempDir()

	// Create many files to test performance
	for i := 0; i < 100; i++ {
		filename := filepath.Join(tempDir, "IPX-"+string(rune(i+1))+".mp4")
		_ = os.WriteFile(filename, []byte("test"), 0644)
	}

	cfg := &config.Config{
		Matching: config.MatchingConfig{
			RegexEnabled: false,
			Extensions:   []string{".mp4"},
		},
	}

	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err)

	// Create minimal ServerDependencies for test
	deps := &ServerDependencies{
		Matcher: mat,
	}
	deps.SetConfig(cfg)

	router := gin.New()
	router.POST("/scan", scanDirectory(deps))

	reqBody := ScanRequest{Path: tempDir}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/scan", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response ScanResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	// Should handle large directory without timeout
	assert.Greater(t, response.Count, 50, "Should process many files")
}

func TestScanDirectory_RecursiveFlag(t *testing.T) {
	// Create directory structure:
	// tempDir/
	//   ├── IPX-001.mp4 (root file)
	//   └── subdir/
	//       └── IPX-002.mp4 (nested file)
	tempDir := t.TempDir()

	// Create root level file
	rootFile := filepath.Join(tempDir, "IPX-001.mp4")
	require.NoError(t, os.WriteFile(rootFile, []byte("test"), 0644))

	// Create subdirectory with nested file
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	nestedFile := filepath.Join(subDir, "IPX-002.mp4")
	require.NoError(t, os.WriteFile(nestedFile, []byte("test"), 0644))

	// Setup deps
	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
				ScanTimeoutSeconds: 30,
				MaxFilesPerScan:    1000,
			},
		},
		Matching: config.MatchingConfig{
			Extensions: []string{".mp4", ".mkv"},
		},
	}

	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/scan", scanDirectory(deps))

	t.Run("non-recursive scan should only find root files", func(t *testing.T) {
		reqBody := ScanRequest{Path: tempDir, Recursive: false}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/scan", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)

		var response ScanResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Non-recursive should only find IPX-001.mp4 (root file)
		assert.Equal(t, 1, response.Count, "Non-recursive scan should find only root-level files")
		if len(response.Files) > 0 {
			assert.Contains(t, response.Files[0].Path, "IPX-001.mp4")
		}
	})

	t.Run("recursive scan should find all files", func(t *testing.T) {
		reqBody := ScanRequest{Path: tempDir, Recursive: true}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/scan", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)

		var response ScanResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Recursive should find both IPX-001.mp4 and IPX-002.mp4
		assert.Equal(t, 2, response.Count, "Recursive scan should find files in subdirectories")

		// Verify both files are found
		paths := make([]string, len(response.Files))
		for i, f := range response.Files {
			paths[i] = f.Path
		}
		assert.True(t, containsPath(paths, "IPX-001.mp4"), "Should find root file")
		assert.True(t, containsPath(paths, "IPX-002.mp4"), "Should find nested file")
	})
}

func containsPath(paths []string, substr string) bool {
	for _, p := range paths {
		if strings.Contains(p, substr) {
			return true
		}
	}
	return false
}

func TestScanDirectory_FilterFlag(t *testing.T) {
	// Create directory structure:
	// tempDir/
	//   ├── IPX-001.mp4 (root file - matches filter "IPX")
	//   ├── ABC-001.mp4 (root file - doesn't match filter "IPX")
	//   ├── IPX-folder/
	//   │   └── IPX-002.mp4 (nested - folder matches filter)
	//   └── OTHER-folder/
	//       └── IPX-003.mp4 (nested - folder doesn't match filter, skipped)
	tempDir := t.TempDir()

	// Create root level files
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "IPX-001.mp4"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "ABC-001.mp4"), []byte("test"), 0644))

	// Create IPX-folder with nested file
	ipxDir := filepath.Join(tempDir, "IPX-folder")
	require.NoError(t, os.MkdirAll(ipxDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(ipxDir, "IPX-002.mp4"), []byte("test"), 0644))

	// Create OTHER-folder with nested file (should be skipped when filtering)
	otherDir := filepath.Join(tempDir, "OTHER-folder")
	require.NoError(t, os.MkdirAll(otherDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "IPX-003.mp4"), []byte("test"), 0644))

	// Setup deps
	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
				ScanTimeoutSeconds: 30,
				MaxFilesPerScan:    1000,
			},
		},
		Matching: config.MatchingConfig{
			Extensions: []string{".mp4", ".mkv"},
		},
	}

	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/scan", scanDirectory(deps))

	t.Run("recursive scan with filter skips non-matching directories", func(t *testing.T) {
		reqBody := ScanRequest{Path: tempDir, Recursive: true, Filter: "IPX"}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/scan", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)

		var response ScanResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should find: IPX-001.mp4 (root), IPX-002.mp4 (in IPX-folder)
		// Should NOT find: ABC-001.mp4 (doesn't match filter), IPX-003.mp4 (in OTHER-folder which doesn't match)
		assert.Equal(t, 2, response.Count, "Filter should only find files in matching directories")

		paths := make([]string, len(response.Files))
		for i, f := range response.Files {
			paths[i] = f.Path
		}
		assert.True(t, containsPath(paths, "IPX-001.mp4"), "Should find IPX-001.mp4 in root (matches filter)")
		assert.True(t, containsPath(paths, "IPX-002.mp4"), "Should find IPX-002.mp4 in IPX-folder")
		assert.False(t, containsPath(paths, "ABC-001.mp4"), "Should NOT find ABC-001.mp4 (doesn't match filter)")
		assert.False(t, containsPath(paths, "IPX-003.mp4"), "Should NOT find IPX-003.mp4 (in OTHER-folder which doesn't match)")
	})

	t.Run("recursive scan without filter finds all files", func(t *testing.T) {
		reqBody := ScanRequest{Path: tempDir, Recursive: true, Filter: ""}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/scan", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)

		var response ScanResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should find all 4 files
		assert.Equal(t, 4, response.Count, "No filter should find all files")
	})

	t.Run("filter is case insensitive", func(t *testing.T) {
		reqBody := ScanRequest{Path: tempDir, Recursive: true, Filter: "ipx"} // lowercase
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/scan", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)

		var response ScanResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should still find IPX files (case insensitive)
		assert.Equal(t, 2, response.Count, "Lowercase filter should match uppercase directories/files")
	})
}
