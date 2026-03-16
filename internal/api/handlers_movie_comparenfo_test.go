package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareNFO_Security(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup test directories
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	forbiddenDir := filepath.Join(tempDir, "forbidden")

	require.NoError(t, os.Mkdir(allowedDir, 0755))
	require.NoError(t, os.Mkdir(forbiddenDir, 0755))

	// Create NFO files
	allowedNFO := filepath.Join(allowedDir, "IPX-001.nfo")
	forbiddenNFO := filepath.Join(forbiddenDir, "IPX-001.nfo")
	nfoContent := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
	<title>Test Movie</title>
	<id>IPX-001</id>
	<year>2023</year>
</movie>`

	require.NoError(t, os.WriteFile(allowedNFO, []byte(nfoContent), 0644))
	require.NoError(t, os.WriteFile(forbiddenNFO, []byte(nfoContent), 0644))

	tests := []struct {
		name           string
		movieID        string
		nfoPath        string
		allowedDirs    []string
		expectedStatus int
		checkError     func(*testing.T, ErrorResponse)
	}{
		{
			name:           "access denied - path outside allowed directory",
			movieID:        "IPX-001",
			nfoPath:        forbiddenNFO,
			allowedDirs:    []string{allowedDir},
			expectedStatus: 403,
			checkError: func(t *testing.T, err ErrorResponse) {
				assert.Contains(t, err.Error, "access denied")
			},
		},
		{
			name:           "not found - non-existent file",
			movieID:        "IPX-001",
			nfoPath:        filepath.Join(allowedDir, "nonexistent.nfo"),
			allowedDirs:    []string{allowedDir},
			expectedStatus: 404,
			checkError: func(t *testing.T, err ErrorResponse) {
				assert.Contains(t, err.Error, "not found")
			},
		},
		{
			name:           "bad request - directory instead of file",
			movieID:        "IPX-001",
			nfoPath:        allowedDir,
			allowedDirs:    []string{tempDir},
			expectedStatus: 400,
			checkError: func(t *testing.T, err ErrorResponse) {
				assert.Contains(t, err.Error, "directory")
			},
		},
		{
			name:           "bad request - missing nfo_path",
			movieID:        "IPX-001",
			nfoPath:        "",
			allowedDirs:    []string{allowedDir},
			expectedStatus: 400,
			checkError: func(t *testing.T, err ErrorResponse) {
				assert.Contains(t, err.Error, "required")
			},
		},
		{
			name:           "path traversal attempt blocked",
			movieID:        "IPX-001",
			nfoPath:        filepath.Join(allowedDir, "..", "forbidden", "IPX-001.nfo"),
			allowedDirs:    []string{allowedDir},
			expectedStatus: 403,
			checkError: func(t *testing.T, err ErrorResponse) {
				assert.Contains(t, err.Error, "access denied")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test dependencies
			cfg := &config.Config{
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedDirectories: tt.allowedDirs,
					},
				},
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev"},
				},
			}

			deps := createTestDeps(t, cfg, "")

			// Create request
			reqBody := NFOComparisonRequest{
				NFOPath: tt.nfoPath,
			}
			bodyBytes, _ := json.Marshal(reqBody)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", fmt.Sprintf("/api/v1/movies/%s/compare-nfo", tt.movieID), bytes.NewReader(bodyBytes))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: tt.movieID}}

			// Execute
			compareNFO(deps)(c)

			// Verify
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.checkError != nil {
				var errResp ErrorResponse
				err := json.Unmarshal(w.Body.Bytes(), &errResp)
				require.NoError(t, err)
				tt.checkError(t, errResp)
			}
		})
	}
}

func TestCompareNFO_ValidComparison(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup test directory and NFO file
	tempDir := t.TempDir()
	nfoPath := filepath.Join(tempDir, "IPX-535.nfo")
	nfoContent := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
	<title>NFO Title</title>
	<id>IPX-535</id>
	<year>2023</year>
	<studio>NFO Studio</studio>
	<plot>NFO plot description</plot>
	<actor>
		<name>NFO Actress 1</name>
	</actor>
</movie>`

	require.NoError(t, os.WriteFile(nfoPath, []byte(nfoContent), 0644))

	// Setup test dependencies
	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
			},
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")

	// Mock scraper result
	now := time.Now()
	mockScraper := &mockScraperWithResults{
		name:    "r18dev",
		enabled: true,
		result: &models.ScraperResult{
			ID:          "IPX-535",
			ContentID:   "IPX-535",
			Title:       "Scraped Title",
			Maker:       "Scraped Maker",
			ReleaseDate: &now,
			Description: "Scraped plot description",
			Actresses: []models.ActressInfo{
				{FirstName: "Scraped Actress 1"},
				{FirstName: "Scraped Actress 2"},
			},
			Source: "r18dev",
		},
	}
	deps.GetRegistry().Register(mockScraper)

	tests := []struct {
		name          string
		mergeStrategy string
		checkResponse func(*testing.T, *NFOComparisonResponse)
	}{
		{
			name:          "prefer-scraper strategy",
			mergeStrategy: "prefer-scraper",
			checkResponse: func(t *testing.T, resp *NFOComparisonResponse) {
				assert.True(t, resp.NFOExists)
				assert.NotNil(t, resp.NFOData)
				assert.NotNil(t, resp.ScrapedData)
				assert.NotNil(t, resp.MergedData)
				assert.NotNil(t, resp.Provenance)
				assert.NotNil(t, resp.MergeStats)

				// With prefer-scraper, scraped data should win
				assert.Equal(t, "Scraped Title", resp.MergedData.Title)
				assert.Equal(t, "Scraped Maker", resp.MergedData.Maker)

				// Verify provenance tracking (keys are lowercase)
				assert.Contains(t, resp.Provenance, "title")
				assert.Equal(t, "scraper", resp.Provenance["title"].Source)

				// NFO path should only contain filename (security)
				assert.Equal(t, "IPX-535.nfo", resp.NFOPath)
				assert.NotContains(t, resp.NFOPath, tempDir)
			},
		},
		{
			name:          "prefer-nfo strategy",
			mergeStrategy: "prefer-nfo",
			checkResponse: func(t *testing.T, resp *NFOComparisonResponse) {
				// With prefer-nfo, NFO data should be preferred when available
				assert.NotNil(t, resp.NFOData)
				assert.NotNil(t, resp.ScrapedData)
				assert.NotNil(t, resp.MergedData)

				// Verify merge happened
				assert.NotNil(t, resp.MergeStats)
				assert.Greater(t, resp.MergeStats.TotalFields, 0)
			},
		},
		{
			name:          "merge-arrays strategy",
			mergeStrategy: "merge-arrays",
			checkResponse: func(t *testing.T, resp *NFOComparisonResponse) {
				// With merge-arrays, arrays should be combined
				// Actresses should include both NFO and scraped
				assert.GreaterOrEqual(t, len(resp.MergedData.Actresses), 1)

				// Check merge stats
				assert.NotNil(t, resp.MergeStats)
				assert.Greater(t, resp.MergeStats.TotalFields, 0)
			},
		},
		{
			name:          "default strategy (empty string)",
			mergeStrategy: "",
			checkResponse: func(t *testing.T, resp *NFOComparisonResponse) {
				// Default for compareNFO should be prefer-nfo (conservative, preserves existing data)
				assert.Equal(t, "NFO Title", resp.MergedData.Title)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request
			reqBody := NFOComparisonRequest{
				NFOPath:       nfoPath,
				MergeStrategy: tt.mergeStrategy,
			}
			bodyBytes, _ := json.Marshal(reqBody)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/v1/movies/IPX-535/compare-nfo", bytes.NewReader(bodyBytes))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: "IPX-535"}}

			// Execute
			compareNFO(deps)(c)

			// Verify
			assert.Equal(t, 200, w.Code)

			var resp NFOComparisonResponse
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)

			tt.checkResponse(t, &resp)
		})
	}
}

func TestCompareNFO_ErrorCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()

	tests := []struct {
		name           string
		setupNFO       func() string
		movieID        string
		mergeStrategy  string
		setupScraper   func(*models.ScraperRegistry)
		expectedStatus int
		checkError     func(*testing.T, ErrorResponse)
	}{
		{
			name: "invalid merge strategy",
			setupNFO: func() string {
				nfoPath := filepath.Join(tempDir, "test.nfo")
				content := `<?xml version="1.0" encoding="UTF-8"?><movie><title>Test</title></movie>`
				_ = os.WriteFile(nfoPath, []byte(content), 0644)
				return nfoPath
			},
			movieID:       "IPX-001",
			mergeStrategy: "invalid-strategy",
			setupScraper: func(registry *models.ScraperRegistry) {
				// Register a working scraper so we get to the merge strategy validation
				now := time.Now()
				mockScraper := &mockScraperWithResults{
					name:    "r18dev",
					enabled: true,
					result: &models.ScraperResult{
						ID:          "IPX-001",
						ContentID:   "IPX-001",
						Title:       "Test Title",
						ReleaseDate: &now,
						Source:      "r18dev",
					},
				}
				registry.Register(mockScraper)
			},
			expectedStatus: 400,
			checkError: func(t *testing.T, err ErrorResponse) {
				assert.Contains(t, err.Error, "Invalid merge strategy")
			},
		},
		{
			name: "malformed NFO XML",
			setupNFO: func() string {
				nfoPath := filepath.Join(tempDir, "malformed.nfo")
				content := `<movie><title>Unclosed tag`
				_ = os.WriteFile(nfoPath, []byte(content), 0644)
				return nfoPath
			},
			movieID:        "IPX-001",
			mergeStrategy:  "prefer-scraper",
			expectedStatus: 500,
			checkError: func(t *testing.T, err ErrorResponse) {
				assert.Contains(t, err.Error, "Failed to parse NFO")
			},
		},
		{
			name: "no scraped data available",
			setupNFO: func() string {
				nfoPath := filepath.Join(tempDir, "noscrape.nfo")
				content := `<?xml version="1.0" encoding="UTF-8"?><movie><title>Test</title></movie>`
				_ = os.WriteFile(nfoPath, []byte(content), 0644)
				return nfoPath
			},
			movieID:       "INVALID-999",
			mergeStrategy: "prefer-scraper",
			setupScraper: func(registry *models.ScraperRegistry) {
				// Register a scraper that will fail
				mockScraper := &mockScraperWithResults{
					name:    "r18dev",
					enabled: true,
					err:     fmt.Errorf("scraper error"),
				}
				registry.Register(mockScraper)
			},
			expectedStatus: 404,
			checkError: func(t *testing.T, err ErrorResponse) {
				assert.Contains(t, err.Error, "No scraped data available")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			cfg := &config.Config{
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedDirectories: []string{tempDir},
					},
				},
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev"},
				},
			}

			deps := createTestDeps(t, cfg, "")

			if tt.setupScraper != nil {
				tt.setupScraper(deps.GetRegistry())
			}

			nfoPath := tt.setupNFO()

			// Create request
			reqBody := NFOComparisonRequest{
				NFOPath:       nfoPath,
				MergeStrategy: tt.mergeStrategy,
			}
			bodyBytes, _ := json.Marshal(reqBody)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", fmt.Sprintf("/api/v1/movies/%s/compare-nfo", tt.movieID), bytes.NewReader(bodyBytes))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: tt.movieID}}

			// Execute
			compareNFO(deps)(c)

			// Verify
			assert.Equal(t, tt.expectedStatus, w.Code)

			var errResp ErrorResponse
			err := json.Unmarshal(w.Body.Bytes(), &errResp)
			require.NoError(t, err)
			tt.checkError(t, errResp)
		})
	}
}

func TestCompareNFO_ProvenanceTracking(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// This test verifies that the pointer aliasing bug is fixed
	// All provenance timestamps should be unique, not pointing to the same memory

	tempDir := t.TempDir()
	nfoPath := filepath.Join(tempDir, "IPX-001.nfo")
	nfoContent := `<?xml version="1.0" encoding="UTF-8"?>
<movie>
	<title>NFO Title</title>
	<id>IPX-001</id>
	<year>2023</year>
	<studio>Studio</studio>
</movie>`

	require.NoError(t, os.WriteFile(nfoPath, []byte(nfoContent), 0644))

	// Setup
	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
			},
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")

	// Mock scraper with specific timestamps
	now := time.Now()
	mockScraper := &mockScraperWithResults{
		name:    "r18dev",
		enabled: true,
		result: &models.ScraperResult{
			ID:          "IPX-001",
			ContentID:   "IPX-001",
			Title:       "Scraped Title",
			Maker:       "Scraped Maker",
			ReleaseDate: &now,
			Source:      "r18dev",
		},
	}
	deps.GetRegistry().Register(mockScraper)

	// Create request
	reqBody := NFOComparisonRequest{
		NFOPath:       nfoPath,
		MergeStrategy: "prefer-scraper",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/movies/IPX-001/compare-nfo", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "IPX-001"}}

	// Execute
	compareNFO(deps)(c)

	// Verify
	assert.Equal(t, 200, w.Code)

	var resp NFOComparisonResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	// CRITICAL: Verify each field has its own timestamp pointer
	// This tests the fix for the string pointer reuse bug
	timestamps := make(map[string]*string)
	for field, prov := range resp.Provenance {
		if prov.LastUpdated != nil {
			timestamps[field] = prov.LastUpdated
		}
	}

	// Verify we have multiple fields with timestamps
	assert.Greater(t, len(timestamps), 1, "Should have multiple fields with timestamps")

	// Verify each pointer is unique (not aliasing)
	pointerAddresses := make(map[uintptr]bool)
	for field, ts := range timestamps {
		addr := uintptr(unsafe.Pointer(ts))
		if pointerAddresses[addr] {
			t.Errorf("Field %s has duplicate pointer address - pointer aliasing bug detected!", field)
		}
		pointerAddresses[addr] = true
	}
}
