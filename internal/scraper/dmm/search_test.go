package dmm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseHTMLWithGoldenFiles tests the parseHTML method using golden HTML files.
// This tests the core scraping logic without requiring HTTP mocking.
func TestParseHTMLWithGoldenFiles(t *testing.T) {
	tests := []struct {
		name             string
		goldenFile       string
		sourceURL        string
		wantContentID    string
		wantTitle        string
		wantActressCount int
		wantGenreCount   int
		wantError        bool
		errorContains    string
	}{
		{
			name:             "success - complete movie data",
			goldenFile:       "dmm_search_success.html.golden",
			sourceURL:        "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=abc00123/",
			wantContentID:    "abc00123",
			wantTitle:        "成功ムービー ABC-123",
			wantActressCount: 2, // Based on golden file content
			wantGenreCount:   3, // Based on golden file content
			wantError:        false,
		},
		{
			name:             "partial data - missing actress",
			goldenFile:       "dmm_search_partial.html.golden",
			sourceURL:        "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=def00456/",
			wantContentID:    "def00456",
			wantTitle:        "パーシャルムービー DEF-456",
			wantActressCount: 0, // No actresses in this golden file
			wantGenreCount:   1, // Only 1 genre
			wantError:        false,
		},
		{
			name:             "multi actress - large cast",
			goldenFile:       "dmm_search_multi_actress.html.golden",
			sourceURL:        "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ghi00789/",
			wantContentID:    "ghi00789",
			wantTitle:        "マルチキャストムービー GHI-789",
			wantActressCount: 7, // 7 actresses in golden file
			wantGenreCount:   2, // 2 genres
			wantError:        false,
		},
		{
			name:             "404 error page",
			goldenFile:       "dmm_404_not_found.html.golden",
			sourceURL:        "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=notfound/",
			wantContentID:    "notfound",
			wantTitle:        "", // 404 page won't have proper title
			wantActressCount: 0,
			wantGenreCount:   0,
			wantError:        false, // parseHTML doesn't error on 404, just returns empty data
		},
		{
			name:             "malformed HTML - broken tags",
			goldenFile:       "dmm_malformed_html.html.golden",
			sourceURL:        "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=jkl00999/",
			wantContentID:    "jkl00999",
			wantTitle:        "", // goquery can't extract title from severely malformed HTML
			wantActressCount: 0,  // goquery can't reliably extract from malformed structure
			wantGenreCount:   0,
			wantError:        false, // goquery handles malformed HTML gracefully (no panic/error)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Load golden HTML file from local testdata directory
			htmlContent, err := os.ReadFile(filepath.Join("testdata", tt.goldenFile))
			require.NoError(t, err, "Failed to load golden file")

			// Parse HTML into goquery document
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(htmlContent)))
			require.NoError(t, err, "Failed to parse HTML")

			// Create scraper instance with proper initialization
			settings := config.ScraperSettings{
				Enabled:       true,
				ScrapeActress: ptrBool(true),
			}
			scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, true, false), nil)

			// Call parseHTML
			result, err := scraper.parseHTML(context.Background(), doc, tt.sourceURL)

			// Check error expectations
			if tt.wantError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			// Verify basic fields
			assert.Equal(t, "dmm", result.Source)
			assert.Equal(t, tt.sourceURL, result.SourceURL)
			assert.Equal(t, "ja", result.Language)

			// Verify content ID extraction
			if tt.wantContentID != "" {
				assert.Equal(t, tt.wantContentID, result.ContentID)
			}

			// Verify title extraction
			if tt.wantTitle != "" {
				assert.Equal(t, tt.wantTitle, result.Title)
			}

			// Verify actress count
			if tt.wantActressCount > 0 {
				require.NotNil(t, result.Actresses)
				assert.Len(t, result.Actresses, tt.wantActressCount)
			}

			// Verify genre count
			if tt.wantGenreCount > 0 {
				require.NotNil(t, result.Genres)
				assert.Len(t, result.Genres, tt.wantGenreCount)
			}
		})
	}
}

// TestParseHTMLFieldExtraction tests specific field extraction logic.
func TestParseHTMLFieldExtraction(t *testing.T) {
	// Load the success golden file for detailed field testing
	htmlContent, err := os.ReadFile(filepath.Join("testdata", "dmm_search_success.html.golden"))
	require.NoError(t, err)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(htmlContent)))
	require.NoError(t, err)

	settings := config.ScraperSettings{
		Enabled:       true,
		ScrapeActress: ptrBool(true),
	}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, true, false), nil)

	result, err := scraper.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=abc00123/")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Test specific field extractions based on golden file content
	t.Run("title extracted", func(t *testing.T) {
		assert.NotEmpty(t, result.Title)
		assert.Equal(t, result.Title, result.OriginalTitle)
	})

	t.Run("content ID extracted from URL", func(t *testing.T) {
		assert.Equal(t, "abc00123", result.ContentID)
	})

	t.Run("ID normalized from content ID", func(t *testing.T) {
		assert.NotEmpty(t, result.ID)
		// ID should be normalized form of ContentID
		assert.Contains(t, strings.ToLower(result.ID), "abc")
	})

	t.Run("actresses extracted", func(t *testing.T) {
		require.NotNil(t, result.Actresses)
		assert.Greater(t, len(result.Actresses), 0)
		// Verify actress data structure - actresses should have either FirstName or JapaneseName populated
		for _, actress := range result.Actresses {
			hasName := actress.FirstName != "" || actress.JapaneseName != ""
			assert.True(t, hasName, "Actress should have either FirstName or JapaneseName populated")
		}
	})

	t.Run("genres extracted", func(t *testing.T) {
		require.NotNil(t, result.Genres)
		assert.Greater(t, len(result.Genres), 0)
		// Verify no empty genre strings
		for _, genre := range result.Genres {
			assert.NotEmpty(t, genre, "Genre should not be empty string")
		}
	})

	t.Run("cover URL extracted", func(t *testing.T) {
		if result.CoverURL != "" {
			assert.Contains(t, result.CoverURL, "http", "Cover URL should be a valid URL")
		}
	})

	t.Run("description extracted", func(t *testing.T) {
		// Description may be empty, but if present should not be just whitespace
		if result.Description != "" {
			assert.NotEqual(t, strings.TrimSpace(result.Description), "")
		}
	})
}

// TestParseHTMLActressDisabled tests that actress scraping respects the configuration.
func TestParseHTMLActressDisabled(t *testing.T) {
	htmlContent, err := os.ReadFile(filepath.Join("testdata", "dmm_search_success.html.golden"))
	require.NoError(t, err)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(htmlContent)))
	require.NoError(t, err)

	// Create scraper with actress scraping DISABLED
	settings := config.ScraperSettings{
		Enabled: true,
	}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), nil)

	result, err := scraper.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=abc00123/")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Actresses should be nil or empty when scrapeActress is false
	if result.Actresses != nil {
		assert.Len(t, result.Actresses, 0, "Actresses should be empty when scraping is disabled")
	}
}

// TestParseHTMLMultipleActresses tests extraction of multiple actresses.
func TestParseHTMLMultipleActresses(t *testing.T) {
	htmlContent, err := os.ReadFile(filepath.Join("testdata", "dmm_search_multi_actress.html.golden"))
	require.NoError(t, err)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(htmlContent)))
	require.NoError(t, err)

	settings := config.ScraperSettings{
		Enabled:       true,
		ScrapeActress: ptrBool(true),
	}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, true, false), nil)

	result, err := scraper.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ghi00789/")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify we extracted all 7 actresses
	require.NotNil(t, result.Actresses)
	assert.Len(t, result.Actresses, 7, "Should extract all 7 actresses from multi-actress movie")

	// Verify actress names are unique (no duplicates)
	seen := make(map[string]bool)
	for _, actress := range result.Actresses {
		assert.NotEmpty(t, actress.FirstName)
		assert.False(t, seen[actress.FirstName], "Actress names should be unique")
		seen[actress.FirstName] = true
	}
}

// TestParseHTMLEmptyActressList tests handling of movies with no actresses.
func TestParseHTMLEmptyActressList(t *testing.T) {
	htmlContent, err := os.ReadFile(filepath.Join("testdata", "dmm_search_partial.html.golden"))
	require.NoError(t, err)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(htmlContent)))
	require.NoError(t, err)

	settings := config.ScraperSettings{
		Enabled:       true,
		ScrapeActress: ptrBool(true),
	}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, true, false), nil)

	result, err := scraper.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=def00456/")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Partial golden file has no actresses
	if result.Actresses != nil {
		assert.Len(t, result.Actresses, 0, "Partial movie should have no actresses")
	}
}

// TestParseHTMLMalformedHTML tests robustness against broken HTML.
func TestParseHTMLMalformedHTML(t *testing.T) {
	htmlContent, err := os.ReadFile(filepath.Join("testdata", "dmm_malformed_html.html.golden"))
	require.NoError(t, err)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(htmlContent)))
	require.NoError(t, err)

	settings := config.ScraperSettings{
		Enabled:       true,
		ScrapeActress: ptrBool(true),
	}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, true, false), nil)

	// parseHTML should not panic or error on malformed HTML
	result, err := scraper.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=jkl00999/")

	assert.NoError(t, err, "parseHTML should handle malformed HTML gracefully")
	require.NotNil(t, result, "Should return a result even with malformed HTML")

	// Verify no panic occurred - that's the main test for robustness
	// Note: Severely malformed HTML may not extract data correctly, but shouldn't crash
	assert.Equal(t, "dmm", result.Source)
	assert.Equal(t, "ja", result.Language)
}

// TestParseHTML404Page tests handling of 404 error pages.
func TestParseHTML404Page(t *testing.T) {
	htmlContent, err := os.ReadFile(filepath.Join("testdata", "dmm_404_not_found.html.golden"))
	require.NoError(t, err)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(htmlContent)))
	require.NoError(t, err)

	settings := config.ScraperSettings{
		Enabled:       true,
		ScrapeActress: ptrBool(true),
	}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, true, false), nil)

	result, err := scraper.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=notfound/")

	// parseHTML should not error on 404 pages (HTTP layer should catch 404)
	assert.NoError(t, err)
	require.NotNil(t, result)

	// 404 page won't have proper movie metadata
	assert.Equal(t, "dmm", result.Source)
	assert.Equal(t, "ja", result.Language)
	// Title might be empty or contain error message
	// ContentID should still be extracted from URL
	assert.Equal(t, "notfound", result.ContentID)
}
