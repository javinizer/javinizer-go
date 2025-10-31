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
		setupFiles     func(string) string // Returns path to scan
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *ScanResponse)
	}{
		{
			name: "scan directory with video files",
			setupFiles: func(tempDir string) string {
				// Create test video files
				testFile1 := filepath.Join(tempDir, "IPX-535.mp4")
				testFile2 := filepath.Join(tempDir, "ABC-123.mkv")
				os.WriteFile(testFile1, []byte("test"), 0644)
				os.WriteFile(testFile2, []byte("test"), 0644)
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
			name: "scan non-existent directory",
			setupFiles: func(tempDir string) string {
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
			setupFiles: func(tempDir string) string {
				// Create non-video files
				testFile := filepath.Join(tempDir, "document.txt")
				os.WriteFile(testFile, []byte("test"), 0644)
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
			setupFiles: func(tempDir string) string {
				return tempDir
			},
			requestBody:    map[string]string{},
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			scanPath := tt.setupFiles(tempDir)

			cfg := &config.Config{
				Matching: config.MatchingConfig{
					RegexEnabled: false,
					Extensions:   []string{".mp4", ".mkv", ".avi"},
				},
			}

			mat, err := matcher.NewMatcher(&cfg.Matching)
			require.NoError(t, err)

			router := gin.New()
			router.POST("/scan", scanDirectory(mat, cfg))

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

	router := gin.New()
	router.POST("/scan", scanDirectory(mat, cfg))

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		errorContains  string
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
			expectedStatus: 0,  // Will validate manually
			errorContains:  "", // Will validate manually
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

			// Special handling for path traversal test (environment-dependent behavior)
			if tt.expectedStatus == 0 {
				// Accept either 400 (path doesn't exist) or 403 (system directory blocked)
				assert.True(t, w.Code == 400 || w.Code == 403,
					"Expected 400 or 403 for path traversal, got %d. Response: %s", w.Code, w.Body.String())
				// Verify error message contains security-related keywords
				responseBody := w.Body.String()
				assert.True(t,
					strings.Contains(responseBody, "does not exist") ||
						strings.Contains(responseBody, "system directory") ||
						strings.Contains(responseBody, "access denied"),
					"Expected security-related error message, got: %s", responseBody)
			} else {
				assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())
				if tt.errorContains != "" {
					assert.Contains(t, w.Body.String(), tt.errorContains)
				}
			}
		})
	}
}

func TestGetCurrentWorkingDirectory(t *testing.T) {
	router := gin.New()
	router.GET("/cwd", getCurrentWorkingDirectory())

	req := httptest.NewRequest("GET", "/cwd", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Contains(t, response, "path")
	assert.NotEmpty(t, response["path"])
}

func TestBrowseDirectory(t *testing.T) {
	tests := []struct {
		name           string
		setupFiles     func(string) string // Returns path to browse
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *BrowseResponse)
	}{
		{
			name: "browse directory successfully",
			setupFiles: func(tempDir string) string {
				// Create test files and subdirectories
				os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("test"), 0644)
				os.WriteFile(filepath.Join(tempDir, "file2.mp4"), []byte("test"), 0644)
				os.Mkdir(filepath.Join(tempDir, "subdir"), 0755)
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
			setupFiles: func(tempDir string) string {
				return filepath.Join(tempDir, "nonexistent")
			},
			requestBody: func(path string) BrowseRequest {
				return BrowseRequest{Path: path}
			},
			expectedStatus: 400,
		},
		{
			name: "browse file instead of directory",
			setupFiles: func(tempDir string) string {
				filePath := filepath.Join(tempDir, "file.txt")
				os.WriteFile(filePath, []byte("test"), 0644)
				return filePath
			},
			requestBody: func(path string) BrowseRequest {
				return BrowseRequest{Path: path}
			},
			expectedStatus: 400,
		},
		{
			name: "browse with empty path defaults to cwd",
			setupFiles: func(tempDir string) string {
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
			setupFiles: func(tempDir string) string {
				return tempDir
			},
			requestBody:    "invalid json",
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			browsePath := tt.setupFiles(tempDir)

			cfg := config.DefaultConfig()
			router := gin.New()
			router.POST("/browse", browseDirectory(cfg))

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

	router := gin.New()
	router.POST("/browse", browseDirectory(cfg))

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
	os.Mkdir(subDir, 0755)

	cfg := config.DefaultConfig()
	router := gin.New()
	router.POST("/browse", browseDirectory(cfg))

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

func TestScanDirectory_LargeDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large directory test in short mode")
	}

	tempDir := t.TempDir()

	// Create many files to test performance
	for i := 0; i < 100; i++ {
		filename := filepath.Join(tempDir, "IPX-"+string(rune(i+1))+".mp4")
		os.WriteFile(filename, []byte("test"), 0644)
	}

	cfg := &config.Config{
		Matching: config.MatchingConfig{
			RegexEnabled: false,
			Extensions:   []string{".mp4"},
		},
	}

	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err)

	router := gin.New()
	router.POST("/scan", scanDirectory(mat, cfg))

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
