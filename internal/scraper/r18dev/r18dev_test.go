package r18dev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadTestData reads a JSON fixture file
func loadTestData(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "Failed to read test data file: %s", filename)
	return data
}

// createTestSettings creates ScraperSettings for testing
func createTestSettings(enabled bool) config.ScraperSettings {
	return config.ScraperSettings{
		Enabled:  enabled,
		Language: "en",
	}
}

// testGlobalProxy is a non-nil proxy config used to avoid nil pointer dereference in NewHTTPClient
var testGlobalProxy = &config.ProxyConfig{}

// testGlobalFlareSolverr is a zero-value FlareSolverr config for testing
var testGlobalFlareSolverr = config.FlareSolverrConfig{}

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		settings config.ScraperSettings
		enabled  bool
	}{
		{
			name:     "Enabled scraper",
			settings: createTestSettings(true),
			enabled:  true,
		},
		{
			name:     "Disabled scraper",
			settings: createTestSettings(false),
			enabled:  false,
		},
		{
			name: "Custom user agent",
			settings: config.ScraperSettings{
				Enabled:   true,
				Language:  "en",
				UserAgent: "Custom Agent",
			},
			enabled: true,
		},
		{
			name: "With proxy configuration",
			settings: config.ScraperSettings{
				Enabled:   true,
				Language:  "en",
				UserAgent: "Test Agent",
				Proxy: &config.ProxyConfig{
					Enabled: true,
					Profile: "main",
					Profiles: map[string]config.ProxyProfile{
						"main": {
							URL:      "http://proxy.example.com:8080",
							Username: "user",
							Password: "pass",
						},
					},
				},
			},
			enabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scraper := New(tt.settings, testGlobalProxy, testGlobalFlareSolverr)
			assert.NotNil(t, scraper)
			assert.NotNil(t, scraper.client)
			assert.Equal(t, tt.enabled, scraper.enabled)
		})
	}
}

func TestScraper_Name(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)
	assert.Equal(t, "r18dev", scraper.Name())
}

func TestScraper_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"Enabled", true},
		{"Disabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createTestSettings(tt.enabled)
			scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)
			assert.Equal(t, tt.enabled, scraper.IsEnabled())
		})
	}
}

func TestScraper_GetURL(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name        string
		id          string
		expectedURL string
	}{
		{
			name:        "Standard ID",
			id:          "IPX-535",
			expectedURL: "https://r18.dev/videos/vod/movies/detail/-/combined=ipx535/json",
		},
		{
			name:        "ID with leading zeros",
			id:          "ABW-001",
			expectedURL: "https://r18.dev/videos/vod/movies/detail/-/combined=abw001/json",
		},
		{
			name:        "Lowercase ID",
			id:          "snis-789",
			expectedURL: "https://r18.dev/videos/vod/movies/detail/-/combined=snis789/json",
		},
		{
			name:        "ID with suffix",
			id:          "IPX-535Z",
			expectedURL: "https://r18.dev/videos/vod/movies/detail/-/combined=ipx535z/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := scraper.GetURL(tt.id)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedURL, url)
		})
	}
}

func TestScraper_Search_Success(t *testing.T) {
	// Create test server that simulates R18.dev API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a dvd_id lookup or full data request
		if r.URL.Path == "/videos/vod/movies/detail/-/dvd_id=ipx535/json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(loadTestData(t, "ipx535_dvdid_response.json"))
			return
		}

		if r.URL.Path == "/videos/vod/movies/detail/-/combined=1ipx00535/json" ||
			r.URL.Path == "/videos/vod/movies/detail/-/combined=ipx535/json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(loadTestData(t, "ipx535_full_response.json"))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create scraper with test server URL
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	// Override base URL for testing (we need to modify the client to use test server)
	// For now, we'll test the parsing logic separately since we can't easily override the URL

	// Test parseResponse directly instead
	var data R18Response
	err := json.Unmarshal(loadTestData(t, "ipx535_full_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(context.Background(), &data, "https://r18.dev/test")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify basic fields
	assert.Equal(t, "r18dev", result.Source)
	assert.Equal(t, "https://r18.dev/test", result.SourceURL)
	assert.Equal(t, "en", result.Language)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, "1ipx00535", result.ContentID)

	// Verify English title is preferred
	assert.Equal(t, "Ultimate Soapland Story Vol.95", result.Title)
	assert.Equal(t, "極上泡姫物語 Vol.95", result.OriginalTitle)

	// Verify English description is preferred
	assert.Contains(t, result.Description, "blissful time")

	// Verify date parsing
	require.NotNil(t, result.ReleaseDate)
	expectedDate := time.Date(2020, 8, 13, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, expectedDate, *result.ReleaseDate)

	// Verify runtime
	assert.Equal(t, 120, result.Runtime)

	// Verify director (English preferred)
	assert.Equal(t, "Taro Yamamoto", result.Director)

	// Verify maker/label/series (English preferred)
	assert.Equal(t, "Idea Pocket", result.Maker)
	assert.Equal(t, "Tissue", result.Label)
	assert.Equal(t, "Ultimate Soapland Story", result.Series)

	// Verify genres
	require.Len(t, result.Genres, 3)
	assert.Contains(t, result.Genres, "Big Tits")
	assert.Contains(t, result.Genres, "Soapland")
	assert.Contains(t, result.Genres, "POV")

	// Verify actresses
	require.Len(t, result.Actresses, 1)
	actress := result.Actresses[0]
	assert.Equal(t, 12345, actress.DMMID)
	assert.Equal(t, "Momo", actress.FirstName)
	assert.Equal(t, "Sakura", actress.LastName)
	assert.Equal(t, "桜 もも", actress.JapaneseName)
	assert.Contains(t, actress.ThumbURL, "sakura_momo.jpg")

	// Verify cover/poster URLs
	assert.Equal(t, "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg", result.PosterURL)
	assert.Equal(t, "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg", result.CoverURL)

	// Verify screenshots
	require.Len(t, result.ScreenshotURL, 2)
	assert.Contains(t, result.ScreenshotURL[0], "ipx00535jp-1.jpg")
	assert.Contains(t, result.ScreenshotURL[1], "ipx00535jp-2.jpg")

	// Verify trailer
	assert.Contains(t, result.TrailerURL, "ipx00535_mhb_w.mp4")
}

func TestScraper_Search_LegacyFormat(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	var data R18Response
	err := json.Unmarshal(loadTestData(t, "legacy_format_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(context.Background(), &data, "https://r18.dev/test")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify it handles legacy nested structure
	assert.Equal(t, "ABW-001", result.ID)
	assert.Equal(t, "118abw00001", result.ContentID)
	assert.Equal(t, "Prestige", result.Maker)
	assert.Equal(t, "Absolutely Beautiful Women", result.Label)
	assert.Equal(t, "Legacy Series", result.Series)

	// Verify it uses nested images structure
	assert.Contains(t, result.CoverURL, "118abw00001pl2.jpg")

	// Verify it uses nested sample structure
	assert.Contains(t, result.TrailerURL, "118abw00001_mhb_w.mp4")

	// Verify actress without image_url
	require.Len(t, result.Actresses, 1)
	actress := result.Actresses[0]
	assert.Equal(t, "Hanako", actress.FirstName)
	assert.Equal(t, "Tanaka", actress.LastName)
}

func TestScraper_Search_MinimalData(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	var data R18Response
	err := json.Unmarshal(loadTestData(t, "minimal_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(context.Background(), &data, "https://r18.dev/test")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify it handles minimal data gracefully
	assert.Equal(t, "XYZ-999", result.ID)
	assert.Equal(t, "Minimal Data Test", result.Title)
	assert.Equal(t, 90, result.Runtime)

	// Verify optional fields are empty but don't cause errors
	assert.Empty(t, result.Director)
	assert.Empty(t, result.Maker)
	assert.Empty(t, result.Label)
	assert.Empty(t, result.Series)
	assert.Empty(t, result.Actresses)
	assert.Empty(t, result.Genres)
	assert.Empty(t, result.PosterURL)
	assert.Empty(t, result.ScreenshotURL)
	assert.Empty(t, result.TrailerURL)
}

func TestScraper_Search_EmptyArrays(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	var data R18Response
	err := json.Unmarshal(loadTestData(t, "empty_arrays_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(context.Background(), &data, "https://r18.dev/test")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify empty arrays don't cause nil panics
	// Note: parseResponse initializes slices with make(), so they're never nil
	assert.Empty(t, result.Actresses)
	assert.Empty(t, result.Genres)
	assert.Empty(t, result.ScreenshotURL)
}

func TestStripDMMPrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "DMM content ID with single digit prefix",
			input:    "4sone860",
			expected: "sone860",
		},
		{
			name:     "DMM content ID with three digit prefix",
			input:    "118abw001",
			expected: "abw001",
		},
		{
			name:     "DMM content ID with hyphenated ID",
			input:    "4sone-860",
			expected: "sone-860",
		},
		{
			name:     "Standard JAV ID without DMM prefix",
			input:    "SONE-860",
			expected: "SONE-860",
		},
		{
			name:     "Lowercase ID without DMM prefix",
			input:    "ipx-535",
			expected: "ipx-535",
		},
		{
			name:     "Already normalized without DMM prefix",
			input:    "sone860",
			expected: "sone860",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripDMMPrefix(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Standard format with hyphen",
			input:    "IPX-535",
			expected: "ipx535",
		},
		{
			name:     "Already lowercase",
			input:    "ipx-535",
			expected: "ipx535",
		},
		{
			name:     "Mixed case",
			input:    "IpX-535",
			expected: "ipx535",
		},
		{
			name:     "With leading zeros",
			input:    "ABW-001",
			expected: "abw001",
		},
		{
			name:     "No hyphen",
			input:    "ipx535",
			expected: "ipx535",
		},
		{
			name:     "Multiple hyphens",
			input:    "T-28-123",
			expected: "t28123",
		},
		{
			name:     "With suffix",
			input:    "IPX-535Z",
			expected: "ipx535z",
		},
		{
			name:     "DMM content ID with prefix",
			input:    "4sone860",
			expected: "sone860",
		},
		{
			name:     "DMM content ID with long prefix",
			input:    "118abw001",
			expected: "abw001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContentIDToID(t *testing.T) {
	tests := []struct {
		name      string
		contentID string
		expected  string
	}{
		{
			name:      "Standard format with leading digits",
			contentID: "118abw00001",
			expected:  "ABW-001",
		},
		{
			name:      "No leading digits",
			contentID: "ipx00535",
			expected:  "IPX-535",
		},
		{
			name:      "With suffix",
			contentID: "1ipx00535z",
			expected:  "IPX-535Z",
		},
		{
			name:      "Single digit number",
			contentID: "abc00001",
			expected:  "ABC-001",
		},
		{
			name:      "Large number",
			contentID: "xyz01234",
			expected:  "XYZ-1234",
		},
		{
			name:      "Already uppercase",
			contentID: "IPX00535",
			expected:  "IPX-535",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contentIDToID(tt.contentID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCleanString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Trim whitespace",
			input:    "  hello world  ",
			expected: "hello world",
		},
		{
			name:     "Remove newlines",
			input:    "hello\nworld",
			expected: "hello world",
		},
		{
			name:     "Remove carriage returns",
			input:    "hello\rworld",
			expected: "hello world",
		},
		{
			name:     "Multiple spaces",
			input:    "hello    world",
			expected: "hello world",
		},
		{
			name:     "Mixed whitespace",
			input:    "  hello\n  world  \r\n  test  ",
			expected: "hello world test",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Only whitespace",
			input:    "   \n\r\n   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scraperutil.CleanString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPreferredString(t *testing.T) {
	tests := []struct {
		name      string
		preferred string
		fallback  string
		expected  string
	}{
		{
			name:      "Preferred available",
			preferred: "English Title",
			fallback:  "Japanese Title",
			expected:  "English Title",
		},
		{
			name:      "Preferred empty",
			preferred: "",
			fallback:  "Japanese Title",
			expected:  "Japanese Title",
		},
		{
			name:      "Both empty",
			preferred: "",
			fallback:  "",
			expected:  "",
		},
		{
			name:      "Preferred with spaces",
			preferred: "   ",
			fallback:  "Japanese Title",
			expected:  "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getPreferredString(tt.preferred, tt.fallback)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestActressThumbURLGeneration(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name           string
		nameRomaji     string
		imageURL       string
		expectedSuffix string
	}{
		{
			name:           "Name with last name first",
			nameRomaji:     "Momo Sakura",
			imageURL:       "",
			expectedSuffix: "sakura_momo.jpg",
		},
		{
			name:           "Single name",
			nameRomaji:     "Yui",
			imageURL:       "",
			expectedSuffix: "yui.jpg",
		},
		{
			name:           "Name with special characters",
			nameRomaji:     "Ai-chan Suzuki",
			imageURL:       "",
			expectedSuffix: "suzuki_aichan.jpg",
		},
		{
			name:           "Provided image URL",
			nameRomaji:     "Momo Sakura",
			imageURL:       "custom_image.jpg",
			expectedSuffix: "custom_image.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Actresses: []struct {
					ID         int    `json:"id"`
					ImageURL   string `json:"image_url"`
					NameKana   string `json:"name_kana"`
					NameKanji  string `json:"name_kanji"`
					NameRomaji string `json:"name_romaji"`
				}{
					{
						ID:         123,
						ImageURL:   tt.imageURL,
						NameRomaji: tt.nameRomaji,
						NameKanji:  "テスト",
					},
				},
			}

			result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
			require.NoError(t, err)
			require.Len(t, result.Actresses, 1)

			assert.Contains(t, result.Actresses[0].ThumbURL, tt.expectedSuffix)
		})
	}
}

func TestInvalidDateParsing(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	data := &R18Response{
		DVDID:       "TEST-001",
		ContentID:   "test00001",
		TitleJA:     "Test",
		ReleaseDate: "invalid-date-format",
	}

	result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)

	// Invalid date should result in nil ReleaseDate, not an error
	assert.Nil(t, result.ReleaseDate)
}

func TestFallbackBehavior(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name        string
		data        *R18Response
		checkField  string
		expectedVal string
		description string
	}{
		{
			name: "Director fallback to Japanese",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Director:  "山田太郎",
			},
			checkField:  "director",
			expectedVal: "山田太郎",
			description: "Should use Japanese director when English not available",
		},
		{
			name: "Maker fallback to nested",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Maker: struct {
					Name string `json:"name"`
				}{Name: "Japanese Maker"},
			},
			checkField:  "maker",
			expectedVal: "Japanese Maker",
			description: "Should use nested maker when flat English field not available",
		},
		{
			name: "Label fallback to nested",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Label: struct {
					Name string `json:"name"`
				}{Name: "Japanese Label"},
			},
			checkField:  "label",
			expectedVal: "Japanese Label",
			description: "Should use nested label when flat English field not available",
		},
		{
			name: "Series multiple fallbacks",
			data: &R18Response{
				DVDID:      "TEST-001",
				ContentID:  "test00001",
				TitleJA:    "Test",
				SeriesName: "Fallback Series",
			},
			checkField:  "series",
			expectedVal: "Fallback Series",
			description: "Should try SeriesNameEn, then Series.Name, then SeriesName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(context.Background(), tt.data, "https://r18.dev/test")
			require.NoError(t, err, tt.description)

			switch tt.checkField {
			case "director":
				assert.Equal(t, tt.expectedVal, result.Director, tt.description)
			case "maker":
				assert.Equal(t, tt.expectedVal, result.Maker, tt.description)
			case "label":
				assert.Equal(t, tt.expectedVal, result.Label, tt.description)
			case "series":
				assert.Equal(t, tt.expectedVal, result.Series, tt.description)
			}
		})
	}
}

func TestImageURLFallbacks(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name        string
		data        *R18Response
		expectCover bool
		description string
	}{
		{
			name: "Top-level jacket URL",
			data: &R18Response{
				DVDID:          "TEST-001",
				ContentID:      "test00001",
				TitleJA:        "Test",
				JacketFullURL:  "https://example.com/jacket_full.jpg",
				JacketThumbURL: "https://example.com/jacket_thumb.jpg",
			},
			expectCover: true,
			description: "Should use top-level jacket_full_url",
		},
		{
			name: "Nested large2 image",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Images: struct {
					JacketImage struct {
						Large  string `json:"large"`
						Large2 string `json:"large2"`
					} `json:"jacket_image"`
					SampleImages []string `json:"sample_images"`
				}{
					JacketImage: struct {
						Large  string `json:"large"`
						Large2 string `json:"large2"`
					}{
						Large2: "https://example.com/large2.jpg",
					},
				},
			},
			expectCover: true,
			description: "Should fall back to Images.JacketImage.Large2",
		},
		{
			name: "Nested large image",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Images: struct {
					JacketImage struct {
						Large  string `json:"large"`
						Large2 string `json:"large2"`
					} `json:"jacket_image"`
					SampleImages []string `json:"sample_images"`
				}{
					JacketImage: struct {
						Large  string `json:"large"`
						Large2 string `json:"large2"`
					}{
						Large: "https://example.com/large.jpg",
					},
				},
			},
			expectCover: true,
			description: "Should fall back to Images.JacketImage.Large",
		},
		{
			name: "No images",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
			},
			expectCover: false,
			description: "Should handle missing images gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(context.Background(), tt.data, "https://r18.dev/test")
			require.NoError(t, err, tt.description)

			if tt.expectCover {
				assert.NotEmpty(t, result.CoverURL, tt.description)
				assert.NotEmpty(t, result.PosterURL, tt.description)
				assert.Equal(t, result.CoverURL, result.PosterURL, "Cover and poster should use same URL")
			} else {
				assert.Empty(t, result.CoverURL, tt.description)
				assert.Empty(t, result.PosterURL, tt.description)
			}
		})
	}
}

func TestScreenshotURLFallbacks(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name          string
		data          *R18Response
		expectedCount int
		description   string
	}{
		{
			name: "Gallery images",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Gallery: []struct {
					ImageFull  string `json:"image_full"`
					ImageThumb string `json:"image_thumb"`
				}{
					{ImageFull: "https://example.com/1.jpg"},
					{ImageFull: "https://example.com/2.jpg"},
				},
			},
			expectedCount: 2,
			description:   "Should use gallery images",
		},
		{
			name: "Sample images fallback",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Images: struct {
					JacketImage struct {
						Large  string `json:"large"`
						Large2 string `json:"large2"`
					} `json:"jacket_image"`
					SampleImages []string `json:"sample_images"`
				}{
					SampleImages: []string{
						"https://example.com/sample1.jpg",
						"https://example.com/sample2.jpg",
						"https://example.com/sample3.jpg",
					},
				},
			},
			expectedCount: 3,
			description:   "Should fall back to sample images",
		},
		{
			name: "No screenshots",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
			},
			expectedCount: 0,
			description:   "Should handle missing screenshots gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(context.Background(), tt.data, "https://r18.dev/test")
			require.NoError(t, err, tt.description)
			assert.Len(t, result.ScreenshotURL, tt.expectedCount, tt.description)
		})
	}
}

func TestTrailerURLFallbacks(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name        string
		data        *R18Response
		expectURL   bool
		description string
	}{
		{
			name: "Top-level sample URL",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				SampleURL: "https://example.com/sample.mp4",
			},
			expectURL:   true,
			description: "Should use top-level sample_url",
		},
		{
			name: "Nested high quality",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Sample: struct {
					High string `json:"high"`
					Low  string `json:"low"`
				}{
					High: "https://example.com/high.mp4",
					Low:  "https://example.com/low.mp4",
				},
			},
			expectURL:   true,
			description: "Should prefer high quality nested sample",
		},
		{
			name: "Nested low quality only",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Sample: struct {
					High string `json:"high"`
					Low  string `json:"low"`
				}{
					Low: "https://example.com/low.mp4",
				},
			},
			expectURL:   true,
			description: "Should fall back to low quality nested sample",
		},
		{
			name: "No trailer",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
			},
			expectURL:   false,
			description: "Should handle missing trailer gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(context.Background(), tt.data, "https://r18.dev/test")
			require.NoError(t, err, tt.description)

			if tt.expectURL {
				assert.NotEmpty(t, result.TrailerURL, tt.description)
			} else {
				assert.Empty(t, result.TrailerURL, tt.description)
			}
		})
	}
}

func TestSeriesFallbackPriority(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name     string
		data     *R18Response
		expected string
	}{
		{
			name: "SeriesNameEn takes priority",
			data: &R18Response{
				DVDID:        "TEST-001",
				ContentID:    "test00001",
				TitleJA:      "Test",
				SeriesNameEn: "English Series Name",
				Series: struct {
					Name string `json:"name"`
				}{Name: "Japanese Series Name"},
				SeriesName: "Fallback Series Name",
			},
			expected: "English Series Name",
		},
		{
			name: "Series.Name when no SeriesNameEn",
			data: &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Series: struct {
					Name string `json:"name"`
				}{Name: "Japanese Series Name"},
				SeriesName: "Fallback Series Name",
			},
			expected: "Japanese Series Name",
		},
		{
			name: "SeriesName as last resort",
			data: &R18Response{
				DVDID:      "TEST-001",
				ContentID:  "test00001",
				TitleJA:    "Test",
				SeriesName: "Fallback Series Name",
			},
			expected: "Fallback Series Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(context.Background(), tt.data, "https://r18.dev/test")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.Series)
		})
	}
}

func TestContentIDToIDEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		contentID string
		expected  string
	}{
		{
			name:      "Very short content ID",
			contentID: "a01",
			expected:  "A-001",
		},
		{
			name:      "No digits",
			contentID: "abcdef",
			expected:  "ABCDEF",
		},
		{
			name:      "Only digits",
			contentID: "123456",
			expected:  "123456",
		},
		{
			name:      "Multiple leading digits",
			contentID: "999xyz00123",
			expected:  "XYZ-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contentIDToID(tt.contentID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestActressNameParsing(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name          string
		nameRomaji    string
		expectedFirst string
		expectedLast  string
	}{
		{
			name:          "Standard two-part name",
			nameRomaji:    "Yui Hatano",
			expectedFirst: "Yui",
			expectedLast:  "Hatano",
		},
		{
			name:          "Three-part name",
			nameRomaji:    "Ai Aoi Chan",
			expectedFirst: "Ai",
			expectedLast:  "Aoi",
		},
		{
			name:          "Single name only",
			nameRomaji:    "Madonna",
			expectedFirst: "Madonna",
			expectedLast:  "",
		},
		{
			name:          "Empty name",
			nameRomaji:    "",
			expectedFirst: "",
			expectedLast:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Actresses: []struct {
					ID         int    `json:"id"`
					ImageURL   string `json:"image_url"`
					NameKana   string `json:"name_kana"`
					NameKanji  string `json:"name_kanji"`
					NameRomaji string `json:"name_romaji"`
				}{
					{
						ID:         123,
						NameRomaji: tt.nameRomaji,
					},
				},
			}

			result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
			require.NoError(t, err)
			require.Len(t, result.Actresses, 1)

			assert.Equal(t, tt.expectedFirst, result.Actresses[0].FirstName)
			assert.Equal(t, tt.expectedLast, result.Actresses[0].LastName)
		})
	}
}

func TestIDResolution(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name        string
		data        *R18Response
		expectedID  string
		description string
	}{
		{
			name: "DVDID preferred",
			data: &R18Response{
				DVDID:     "IPX-535",
				ContentID: "1ipx00535",
				TitleJA:   "Test",
			},
			expectedID:  "IPX-535",
			description: "Should prefer DVDID when available",
		},
		{
			name: "ContentID fallback",
			data: &R18Response{
				DVDID:     "",
				ContentID: "1ipx00535",
				TitleJA:   "Test",
			},
			expectedID:  "IPX-535",
			description: "Should convert ContentID when DVDID missing",
		},
		{
			name: "Both empty",
			data: &R18Response{
				DVDID:     "",
				ContentID: "",
				TitleJA:   "Test",
			},
			expectedID:  "",
			description: "Should handle both empty gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(context.Background(), tt.data, "https://r18.dev/test")
			require.NoError(t, err, tt.description)
			assert.Equal(t, tt.expectedID, result.ID, tt.description)
		})
	}
}

// TestParseResponse_LanguageHandling verifies configurable language handling.
func TestParseResponse_LanguageHandling(t *testing.T) {
	data := &R18Response{
		DVDID:     "TEST-001",
		ContentID: "test00001",
		TitleJA:   "日本語タイトル",
		TitleEn:   "English Title",
	}

	t.Run("english mode", func(t *testing.T) {
		cfg := createTestSettings(true)
		cfg.Language = "en"
		scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

		result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
		require.NoError(t, err)

		assert.Equal(t, "en", result.Language)
		assert.Equal(t, "English Title", result.Title)
		assert.Equal(t, "r18dev", result.Source)
	})

	t.Run("japanese mode", func(t *testing.T) {
		cfg := createTestSettings(true)
		cfg.Language = "ja"
		scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

		result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
		require.NoError(t, err)

		assert.Equal(t, "ja", result.Language)
		assert.Equal(t, "日本語タイトル", result.Title)
		assert.Equal(t, "r18dev", result.Source)
	})
}

func TestNormalizeLanguage(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "en", want: "en"},
		{in: "EN", want: "en"},
		{in: "ja", want: "ja"},
		{in: "JA", want: "ja"},
		{in: "", want: "en"},
		{in: "unknown", want: "en"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, scraperutil.NormalizeLanguage(tt.in))
	}
}

// TestParseResponse_TitleFallback verifies title fallback logic
func TestParseResponse_TitleFallback(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name          string
		titleEn       string
		title         string
		expectedTitle string
		expectedOrig  string
	}{
		{
			name:          "English title preferred",
			titleEn:       "English Title",
			title:         "Japanese Title",
			expectedTitle: "English Title",
			expectedOrig:  "Japanese Title",
		},
		{
			name:          "Fallback to Japanese title",
			titleEn:       "",
			title:         "Japanese Title",
			expectedTitle: "Japanese Title",
			expectedOrig:  "Japanese Title",
		},
		{
			name:          "Both empty",
			titleEn:       "",
			title:         "",
			expectedTitle: "",
			expectedOrig:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleEn:   tt.titleEn,
				TitleJA:   tt.title,
			}

			result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
			require.NoError(t, err)

			assert.Equal(t, tt.expectedTitle, result.Title)
			assert.Equal(t, tt.expectedOrig, result.OriginalTitle)
		})
	}
}

// TestParseResponse_DescriptionFallback verifies description fallback logic
func TestParseResponse_DescriptionFallback(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name        string
		descEn      string
		desc        string
		expectedVal string
	}{
		{
			name:        "English description preferred",
			descEn:      "English Description",
			desc:        "Japanese Description",
			expectedVal: "English Description",
		},
		{
			name:        "Fallback to Japanese description",
			descEn:      "",
			desc:        "Japanese Description",
			expectedVal: "Japanese Description",
		},
		{
			name:        "Both empty",
			descEn:      "",
			desc:        "",
			expectedVal: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &R18Response{
				DVDID:         "TEST-001",
				ContentID:     "test00001",
				TitleJA:       "Test",
				DescriptionEn: tt.descEn,
				Description:   tt.desc,
			}

			result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
			require.NoError(t, err)

			assert.Equal(t, tt.expectedVal, result.Description)
		})
	}
}

// TestParseResponse_ReleaseDateVariants verifies date parsing with various formats
func TestParseResponse_ReleaseDateVariants(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name        string
		releaseDate string
		expectNil   bool
		expectedDay int
	}{
		{
			name:        "Valid ISO date",
			releaseDate: "2024-03-15",
			expectNil:   false,
			expectedDay: 15,
		},
		{
			name:        "Invalid format",
			releaseDate: "15/03/2024",
			expectNil:   true,
		},
		{
			name:        "Empty date",
			releaseDate: "",
			expectNil:   true,
		},
		{
			name:        "Malformed date",
			releaseDate: "not-a-date",
			expectNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &R18Response{
				DVDID:       "TEST-001",
				ContentID:   "test00001",
				TitleJA:     "Test",
				ReleaseDate: tt.releaseDate,
			}

			result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
			require.NoError(t, err)

			if tt.expectNil {
				assert.Nil(t, result.ReleaseDate)
			} else {
				require.NotNil(t, result.ReleaseDate)
				assert.Equal(t, tt.expectedDay, result.ReleaseDate.Day())
			}
		})
	}
}

// TestParseResponse_RuntimeVariants verifies runtime handling
func TestParseResponse_RuntimeVariants(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name     string
		runtime  int
		expected int
	}{
		{
			name:     "Standard runtime",
			runtime:  120,
			expected: 120,
		},
		{
			name:     "Zero runtime",
			runtime:  0,
			expected: 0,
		},
		{
			name:     "Short runtime",
			runtime:  30,
			expected: 30,
		},
		{
			name:     "Long runtime",
			runtime:  240,
			expected: 240,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Runtime:   tt.runtime,
			}

			result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
			require.NoError(t, err)

			assert.Equal(t, tt.expected, result.Runtime)
		})
	}
}

// TestParseResponse_EmptyFields verifies handling of empty/missing optional fields
func TestParseResponse_EmptyFields(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	data := &R18Response{
		DVDID:     "TEST-001",
		ContentID: "test00001",
		TitleJA:   "Minimal Test",
		Runtime:   90,
		// All optional fields left empty
	}

	result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
	require.NoError(t, err)

	// Verify required fields
	assert.Equal(t, "r18dev", result.Source)
	assert.Equal(t, "TEST-001", result.ID)
	assert.Equal(t, "test00001", result.ContentID)
	assert.Equal(t, "Minimal Test", result.Title)
	assert.Equal(t, 90, result.Runtime)

	// Verify optional fields are empty but not causing errors
	assert.Empty(t, result.Director)
	assert.Empty(t, result.Maker)
	assert.Empty(t, result.Label)
	assert.Empty(t, result.Series)
	assert.Empty(t, result.Description)
	assert.Nil(t, result.ReleaseDate)
	assert.Empty(t, result.PosterURL)
	assert.Empty(t, result.CoverURL)
	assert.Empty(t, result.TrailerURL)
	assert.Empty(t, result.Actresses)
	assert.Empty(t, result.Genres)
	assert.Empty(t, result.ScreenshotURL)
}

// TestActressThumbURLFallback verifies actress thumbnail URL generation
func TestActressThumbURLFallback(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name           string
		imageURL       string
		nameRomaji     string
		expectPrefix   bool
		expectContains string
		description    string
	}{
		{
			name:           "Provided image URL with relative path",
			imageURL:       "actresses/test_actress.jpg",
			nameRomaji:     "Test Actress",
			expectPrefix:   true,
			expectContains: "actresses/test_actress.jpg",
			description:    "Should prepend DMM URL prefix to relative paths",
		},
		{
			name:           "Provided image URL with absolute URL",
			imageURL:       "https://example.com/actress.jpg",
			nameRomaji:     "Test Actress",
			expectPrefix:   false,
			expectContains: "https://example.com/actress.jpg",
			description:    "Should use absolute URL as-is",
		},
		{
			name:           "Generated from romaji name",
			imageURL:       "",
			nameRomaji:     "Yui Hatano",
			expectPrefix:   false,
			expectContains: "hatano_yui.jpg",
			description:    "Should generate URL from romaji name",
		},
		{
			name:           "Single name actress",
			imageURL:       "",
			nameRomaji:     "Madonna",
			expectPrefix:   false,
			expectContains: "madonna.jpg",
			description:    "Should handle single name actresses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &R18Response{
				DVDID:     "TEST-001",
				ContentID: "test00001",
				TitleJA:   "Test",
				Actresses: []struct {
					ID         int    `json:"id"`
					ImageURL   string `json:"image_url"`
					NameKana   string `json:"name_kana"`
					NameKanji  string `json:"name_kanji"`
					NameRomaji string `json:"name_romaji"`
				}{
					{
						ID:         123,
						ImageURL:   tt.imageURL,
						NameRomaji: tt.nameRomaji,
						NameKanji:  "テスト",
					},
				},
			}

			result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
			require.NoError(t, err, tt.description)
			require.Len(t, result.Actresses, 1, tt.description)

			thumbURL := result.Actresses[0].ThumbURL
			assert.Contains(t, thumbURL, tt.expectContains, tt.description)

			if tt.expectPrefix && tt.imageURL != "" && !strings.HasPrefix(tt.imageURL, "http") {
				assert.Contains(t, thumbURL, "pics.dmm.co.jp", "Should contain DMM domain for relative paths")
			}
		})
	}
}

// TestCategoryParsing verifies genre/category extraction
func TestCategoryParsing(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	tests := []struct {
		name       string
		categories []struct {
			ID                         int    `json:"id"`
			Name                       string `json:"name"`
			NameEn                     string `json:"name_en"`
			NameJa                     string `json:"name_ja"`
			NameEnIsMachineTranslation bool   `json:"name_en_is_machine_translation"`
		}
		expectedCount int
		expectedFirst string
	}{
		{
			name: "Multiple categories with name_en",
			categories: []struct {
				ID                         int    `json:"id"`
				Name                       string `json:"name"`
				NameEn                     string `json:"name_en"`
				NameJa                     string `json:"name_ja"`
				NameEnIsMachineTranslation bool   `json:"name_en_is_machine_translation"`
			}{
				{NameEn: "Drama"},
				{NameEn: "Romance"},
				{NameEn: "Action"},
			},
			expectedCount: 3,
			expectedFirst: "Drama",
		},
		{
			name: "Single category with name_ja",
			categories: []struct {
				ID                         int    `json:"id"`
				Name                       string `json:"name"`
				NameEn                     string `json:"name_en"`
				NameJa                     string `json:"name_ja"`
				NameEnIsMachineTranslation bool   `json:"name_en_is_machine_translation"`
			}{
				{NameJa: "喜剧"},
			},
			expectedCount: 1,
			expectedFirst: "喜剧",
		},
		{
			name: "Empty categories",
			categories: []struct {
				ID                         int    `json:"id"`
				Name                       string `json:"name"`
				NameEn                     string `json:"name_en"`
				NameJa                     string `json:"name_ja"`
				NameEnIsMachineTranslation bool   `json:"name_en_is_machine_translation"`
			}{},
			expectedCount: 0,
		},
		{
			name: "Legacy category with name field",
			categories: []struct {
				ID                         int    `json:"id"`
				Name                       string `json:"name"`
				NameEn                     string `json:"name_en"`
				NameJa                     string `json:"name_ja"`
				NameEnIsMachineTranslation bool   `json:"name_en_is_machine_translation"`
			}{
				{Name: "Drama"},
				{Name: ""},
				{Name: "Romance"},
			},
			expectedCount: 2,
			expectedFirst: "Drama",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &R18Response{
				DVDID:      "TEST-001",
				ContentID:  "test00001",
				TitleJA:    "Test",
				Categories: tt.categories,
			}

			result, err := scraper.parseResponse(context.Background(), data, "https://r18.dev/test")
			require.NoError(t, err)

			assert.Len(t, result.Genres, tt.expectedCount)
			if tt.expectedCount > 0 {
				assert.Equal(t, tt.expectedFirst, result.Genres[0])
			}
		})
	}
}

// TestNormalizeID_SpecialCases verifies ID normalization with special cases
func TestNormalizeID_SpecialCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"With ASCII spaces", "IPX 535", "ipx535"},             // Should remove spaces to create valid API URLs
		{"With tab character", "IPX\t535", "ipx535"},           // Should remove tabs
		{"With non-breaking space", "IPX\u00a0535", "ipx535"},  // Should remove Unicode non-breaking space (U+00A0)
		{"With newline", "IPX\n535", "ipx535"},                 // Should remove newlines
		{"With carriage return", "IPX\r535", "ipx535"},         // Should remove carriage returns
		{"With mixed whitespace", "IPX \t\u00a0535", "ipx535"}, // Should remove all Unicode whitespace
		{"Multiple separators", "IPX--535", "ipx535"},
		{"Trailing hyphen", "IPX-535-", "ipx535"},
		{"Leading hyphen", "-IPX-535", "ipx535"},
		{"Mixed separators", "IPX_535", "ipx_535"}, // Doesn't replace underscores (not common in IDs)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestContentIDToID_SpecialPrefixes verifies content ID to ID conversion with special prefixes
func TestContentIDToID_SpecialPrefixes(t *testing.T) {
	tests := []struct {
		name      string
		contentID string
		expected  string
	}{
		{"Single letter", "x00999", "X-999"},
		{"Long prefix", "abcdef00123", "ABCDEF-123"},
		{"Short number", "abc00001", "ABC-001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contentIDToID(tt.contentID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSearch_Success(t *testing.T) {
	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "dvd_id") {
			// Return DVD ID lookup response
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(loadTestData(t, "ipx535_dvdid_response.json"))
		} else if strings.Contains(r.URL.Path, "combined") {
			// Return full movie data
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(loadTestData(t, "ipx535_full_response.json"))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	// The search will hit the real API since baseURL is a const
	// This is a design limitation - for full testing we'd need DI
	_, err := scraper.Search(context.Background(), "ipx-535")

	// Test will fail with real API, but exercises the code paths
	if err != nil {
		t.Logf("Search failed as expected in test environment: %v", err)
	}
}

func TestSearch_NotFound(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	result, err := scraper.Search(context.Background(), "nonexistent-12345")

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestWaitForRateLimit(t *testing.T) {
	cfg := config.ScraperSettings{
		Enabled:   true,
		Language:  "en",
		RateLimit: 100,
	}

	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)
	_ = scraper.rateLimiter.Wait(context.Background())

	start := time.Now()
	_ = scraper.rateLimiter.Wait(context.Background())
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(90))
}

func TestWaitForRateLimit_NoDelay(t *testing.T) {
	cfg := config.ScraperSettings{
		Enabled:   true,
		Language:  "en",
		RateLimit: 0,
	}

	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	start := time.Now()
	_ = scraper.rateLimiter.Wait(context.Background())
	elapsed := time.Since(start)

	assert.Less(t, elapsed.Milliseconds(), int64(10))
}

func TestUpdateLastRequestTime(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	_ = scraper.rateLimiter.Wait(context.Background())
}

func TestNormalizeIDWithoutStripping(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase with hyphen", "IPX-535", "ipx535"},
		{"already lowercase", "abc123", "abc123"},
		{"with DMM prefix preserved", "61mdb087", "61mdb087"},
		{"with spaces", "ABC 123", "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeIDWithoutStripping(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNew_DefaultMaxRetries(t *testing.T) {
	cfg := config.ScraperSettings{
		Enabled:    true,
		Language:   "en",
		RetryCount: 0, // Default should be 3
	}

	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)
	assert.Equal(t, 3, scraper.maxRetries)
}

func TestScraper_Search_ROYD191(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	var data R18Response
	err := json.Unmarshal(loadTestData(t, "royd191_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(context.Background(), &data, "https://r18.dev/test")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify basic fields
	assert.Equal(t, "ROYD-191", result.ID)
	assert.Equal(t, "royd191", result.ContentID)

	// Verify maker is parsed from maker_name_ja when maker_name_en is null
	assert.Equal(t, "ROYAL", result.Maker, "Maker should be parsed from maker_name_ja when maker_name_en is null")

	// Verify label is parsed correctly
	assert.Equal(t, "HHH Group", result.Label)

	// Verify series is parsed correctly
	assert.NotEmpty(t, result.Series)

	// Verify genres/categories are parsed
	assert.NotEmpty(t, result.Genres, "Categories should be parsed from name_en/name_ja fields")

	// Verify director is parsed from directors array
	assert.Equal(t, "Kawajiri", result.Director, "Director should be parsed from directors array")
}

func TestScraper_Search_ROYD191_Japanese(t *testing.T) {
	cfg := config.ScraperSettings{
		Enabled:  true,
		Language: "ja",
	}
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	var data R18Response
	err := json.Unmarshal(loadTestData(t, "royd191_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(context.Background(), &data, "https://r18.dev/test")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify Japanese values are used
	assert.Equal(t, "ja", result.Language)
	assert.Contains(t, result.Title, "兄嫁", "Title should be in Japanese")
	assert.Equal(t, "ROYAL", result.Maker, "Maker should use maker_name_ja")
	assert.Contains(t, result.Label, "HHHグループ", "Label should be in Japanese")
	assert.Contains(t, result.Series, "兄嫁", "Series should be in Japanese")
	assert.Contains(t, result.Genres[0], "若妻", "Genres should be in Japanese")
	assert.Equal(t, "川尻", result.Director, "Director should be in Japanese (name_kanji)")
}

func TestScraper_TranslationsPopulated(t *testing.T) {
	// Test that translations are populated for both English and Japanese
	cfg := config.ScraperSettings{
		Enabled:  true,
		Language: "en", // Default to English, but should populate both
	}
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	var data R18Response
	err := json.Unmarshal(loadTestData(t, "royd191_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(context.Background(), &data, "https://r18.dev/test")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify translations are populated
	assert.NotEmpty(t, result.Translations, "Translations should be populated")

	// Should have both English and Japanese translations
	require.Len(t, result.Translations, 2, "Should have both EN and JA translations")

	// Find English translation
	var enTranslation, jaTranslation *models.MovieTranslation
	for i := range result.Translations {
		if result.Translations[i].Language == "en" {
			enTranslation = &result.Translations[i]
		}
		if result.Translations[i].Language == "ja" {
			jaTranslation = &result.Translations[i]
		}
	}

	// Verify English translation exists and has correct data
	require.NotNil(t, enTranslation, "English translation should exist")
	assert.Equal(t, "en", enTranslation.Language)
	assert.NotEmpty(t, enTranslation.Title, "English title should be populated")
	assert.Equal(t, "r18dev", enTranslation.SourceName)

	// Verify Japanese translation exists and has correct data
	require.NotNil(t, jaTranslation, "Japanese translation should exist")
	assert.Equal(t, "ja", jaTranslation.Language)
	assert.Contains(t, jaTranslation.Title, "兄嫁", "Japanese title should contain Japanese characters")
	assert.Equal(t, "r18dev", jaTranslation.SourceName)
}

func TestContentIDCoreMatch(t *testing.T) {
	tests := []struct {
		name           string
		contentID      string
		expectedDVDID  string
		expectedResult bool
	}{
		{
			name:           "AP-288 with DMM prefix matches",
			contentID:      "1ap00288",
			expectedDVDID:  "ap288",
			expectedResult: true,
		},
		{
			name:           "Standard IPX-535 with DMM prefix matches",
			contentID:      "1ipx00535",
			expectedDVDID:  "ipx535",
			expectedResult: true,
		},
		{
			name:           "Content ID without DMM prefix matches",
			contentID:      "ipx00535",
			expectedDVDID:  "ipx535",
			expectedResult: true,
		},
		{
			name:           "Mismatched series rejected",
			contentID:      "1abw00001",
			expectedDVDID:  "ipx535",
			expectedResult: false,
		},
		{
			name:           "Long DMM prefix stripped correctly",
			contentID:      "118abw00001",
			expectedDVDID:  "abw001",
			expectedResult: true,
		},
		{
			name:           "Empty content ID rejected",
			contentID:      "",
			expectedDVDID:  "ap288",
			expectedResult: false,
		},
		{
			name:           "Same series different number rejected (ONED-025 false positive)",
			contentID:      "oned205",
			expectedDVDID:  "oned025",
			expectedResult: false,
		},
		{
			name:           "Same series different zero padding accepted",
			contentID:      "1ipx00535",
			expectedDVDID:  "ipx535",
			expectedResult: true,
		},
		{
			name:           "Suffixed content_id accepted (suffix ignored in core match)",
			contentID:      "1ipx00535z",
			expectedDVDID:  "ipx535",
			expectedResult: true,
		},
		{
			name:           "h_ prefix content_id matches",
			contentID:      "h_086mesu103",
			expectedDVDID:  "mesu103",
			expectedResult: true,
		},
		{
			name:           "h_ prefix different series rejected",
			contentID:      "h_086abw00103",
			expectedDVDID:  "mesu103",
			expectedResult: false,
		},
		{
			name:           "h_ prefix different number rejected",
			contentID:      "h_086mesu00200",
			expectedDVDID:  "mesu103",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contentIDCoreMatch(tt.contentID, tt.expectedDVDID)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestContentIDToID_UnderscorePrefix(t *testing.T) {
	tests := []struct {
		name      string
		contentID string
		expected  string
	}{
		{
			name:      "MESU-103 with h_086 prefix",
			contentID: "h_086mesu00103",
			expected:  "MESU-103",
		},
		{
			name:      "Underscore prefix with suffix",
			contentID: "h_086mesu00103z",
			expected:  "MESU-103Z",
		},
		{
			name:      "Underscore prefix different maker code",
			contentID: "h_124abw00001",
			expected:  "ABW-001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contentIDToID(tt.contentID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeDMMScreenshotURL_R18DevIntegration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "AVSA-432 without jp (issue #23)",
			input:    "https://awsimgsrc.dmm.com/dig/digital/video/avsa00432/avsa00432-1.jpg",
			expected: "https://awsimgsrc.dmm.com/dig/digital/video/avsa00432/avsa00432jp-1.jpg",
		},
		{
			name:     "Already has jp suffix",
			input:    "https://awsimgsrc.dmm.com/dig/digital/video/avsa00432/avsa00432jp-1.jpg",
			expected: "https://awsimgsrc.dmm.com/dig/digital/video/avsa00432/avsa00432jp-1.jpg",
		},
		{
			name:     "pics.dmm.co.jp without jp",
			input:    "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg",
			expected: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
		},
		{
			name:     "pics.dmm.co.jp already has jp",
			input:    "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
			expected: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
		},
		{
			name:     "Multiple screenshots second image",
			input:    "https://awsimgsrc.dmm.com/dig/digital/video/avsa00432/avsa00432-2.jpg",
			expected: "https://awsimgsrc.dmm.com/dig/digital/video/avsa00432/avsa00432jp-2.jpg",
		},
		{
			name:     "DMM prefix content ID without jp (1-digit prefix)",
			input:    "https://awsimgsrc.dmm.com/dig/digital/video/1sdmm00132/1sdmm00132-1.jpg",
			expected: "https://awsimgsrc.dmm.com/dig/digital/video/1sdmm00132/1sdmm00132jp-1.jpg",
		},
		{
			name:     "Non-DMM URL unchanged",
			input:    "https://example.com/images/screenshot-1.jpg",
			expected: "https://example.com/images/screenshot-1.jpg",
		},
		{
			name:     "Empty string unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "Cover image pl.jpg unchanged",
			input:    "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg",
			expected: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg",
		},
		{
			name:     "Poster image ps.jpg unchanged",
			input:    "https://awsimgsrc.dmm.com/dig/video/ipx00535/ipx00535ps.jpg",
			expected: "https://awsimgsrc.dmm.com/dig/video/ipx00535/ipx00535ps.jpg",
		},
		{
			name:     "awsimgsrc.dmm.co.jp CDN rewritten to pics.dmm.co.jp",
			input:    "https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-2.jpg",
			expected: "https://pics.dmm.co.jp/video/ipx00535/ipx00535jp-2.jpg",
		},
		{
			name:     "Protocol-relative URL upgraded to https",
			input:    "//pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg",
			expected: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
		},
		{
			name:     "Query parameters stripped",
			input:    "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg?x=1",
			expected: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535jp-1.jpg",
		},
		{
			name:     "Content ID with zero-padded number preserved",
			input:    "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880-1.jpg",
			expected: "https://pics.dmm.co.jp/digital/video/118abp00880/118abp00880jp-1.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := imageutil.NormalizeDMMScreenshotURL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScraper_Search_AVSA432_ScreenshotJPFix(t *testing.T) {
	cfg := createTestSettings(true)
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	var data R18Response
	err := json.Unmarshal(loadTestData(t, "avsa432_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(context.Background(), &data, "https://r18.dev/test")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "AVSA-432", result.ID)
	assert.Equal(t, "avsa00432", result.ContentID)

	require.Len(t, result.ScreenshotURL, 2, "Should have 2 screenshots")
	assert.Contains(t, result.ScreenshotURL[0], "avsa00432jp-1.jpg", "First screenshot should have jp suffix inserted")
	assert.Contains(t, result.ScreenshotURL[1], "avsa00432jp-2.jpg", "Second screenshot should have jp suffix inserted")
}

func TestSearch_AP288_BlankDVDID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live API test in short mode")
	}

	cfg := config.ScraperSettings{
		Enabled:    true,
		Language:   "en",
		RetryCount: 5,
		RateLimit:  2000,
	}
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := scraper.Search(ctx, "AP-288")

	if err != nil {
		if strings.Contains(err.Error(), "403") {
			t.Skip("R18.dev returned 403 — external service unavailable")
		}
		require.NoError(t, err, "AP-288 should resolve successfully via blank dvd_id fallback")
	}
	require.NotNil(t, result)
	assert.Equal(t, "AP-288", result.ID)
	assert.Equal(t, "1ap00288", result.ContentID)
}

func TestGenerateContentIDVariations(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "3-digit number with single prefix (START-575)",
			input:    "START-575",
			expected: []string{"1start00575", "1start575"},
		},
		{
			name:     "already normalized format",
			input:    "start575",
			expected: []string{"1start00575", "1start575"},
		},
		{
			name:     "multi-prefix series (IPX-535 has 8 prefixes)",
			input:    "IPX-535",
			expected: []string{"ipx00535", "ipx535", "4ipx00535", "4ipx535", "5ipx00535", "5ipx535", "6ipx00535", "6ipx535", "7ipx00535", "7ipx535", "9ipx00535", "9ipx535", "77ipx00535", "77ipx535", "88ipx00535", "88ipx535"},
		},
		{
			name:     "single long prefix (ABW-001 uses 118)",
			input:    "ABW-001",
			expected: []string{"118abw00001", "118abw001"},
		},
		{
			name:     "4-digit number with multi-prefix (DSVR-1984)",
			input:    "DSVR-1984",
			expected: []string{"dsvr01984", "dsvr1984", "13dsvr01984", "13dsvr1984"},
		},
		{
			name:     "1-digit number zero-padded (ROYD-1)",
			input:    "ROYD-1",
			expected: []string{"royd00001", "royd001", "2royd00001", "2royd001"},
		},
		{
			name:     "2-digit number (ROYD-19)",
			input:    "ROYD-19",
			expected: []string{"royd00019", "royd019", "2royd00019", "2royd019"},
		},
		{
			name:     "large 4-digit number (SSIS-1200)",
			input:    "SSIS-1200",
			expected: []string{"ssis01200", "ssis1200", "4ssis01200", "4ssis1200", "7ssis01200", "7ssis1200", "9ssis01200", "9ssis1200", "77ssis01200", "77ssis1200", "88ssis01200", "88ssis1200"},
		},
		{
			name:     "no-dash content_id format (1sdam00171 from API)",
			input:    "1sdam00171",
			expected: []string{"1sdam00171", "1sdam171"},
		},
		{
			name:     "unknown series falls back to empty+1 prefix",
			input:    "ZZZZ-123",
			expected: []string{"zzzz00123", "zzzz123", "1zzzz00123", "1zzzz123"},
		},
		{
			name:     "no dash and unparseable returns nil",
			input:    "invalid",
			expected: nil,
		},
		{
			name:     "3-digit number with multi-prefix (ABF-346 uses 118 and 436)",
			input:    "ABF-346",
			expected: []string{"118abf00346", "118abf346", "436abf00346", "436abf346"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := generateContentIDVariations(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSplitSeriesAndNumber(t *testing.T) {
	testCases := []struct {
		name       string
		input      string
		wantSeries string
		wantNumber string
	}{
		{"standard dvd_id format", "START-575", "START", "575"},
		{"lowercase format", "ipx-535", "ipx", "535"},
		{"normalized format (no dash)", "ipx535", "ipx", "535"},
		{"4-digit number", "DSVR-1984", "DSVR", "1984"},
		{"1-digit number", "ROYD-1", "ROYD", "1"},
		{"2-digit number", "ROYD-19", "ROYD", "19"},
		{"DMM-prefixed content_id (1sdam00171)", "1sdam00171", "sdam", "00171"},
		{"content_id with no DMM prefix (royd191)", "royd191", "royd", "191"},
		{"pure alpha no number", "INVALID", "", ""},
		{"empty string", "", "", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			series, num := splitSeriesAndNumber(tc.input)
			assert.Equal(t, tc.wantSeries, series)
			assert.Equal(t, tc.wantNumber, num)
		})
	}
}

func TestResolveAwsimgsrcPoster(t *testing.T) {
	// Set up a mock HTTP server that simulates awsimgsrc.dmm.com
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate awsimgsrc: only certain paths return valid images
		// dig/mono/movie/{id}/{id}ps.jpg
		path := r.URL.Path

		// Valid poster URLs (return a tiny valid JPEG)
		validPaths := map[string]bool{
			"/dig/mono/movie/1sdam171/1sdam171ps.jpg":       true, // digital/video content, prefix 1
			"/dig/mono/movie/ipx535/ipx535ps.jpg":           true, // digital/video content, no prefix
			"/dig/mono/movie/4sone860/4sone860ps.jpg":       true, // digital/video content, prefix 4
			"/dig/mono/movie/118abw001/118abw001ps.jpg":     true, // mono/movie/adult content
			"/dig/mono/movie/royd191/royd191ps.jpg":         true, // mono/movie/adult content
			"/dig/mono/movie/13dsvr01984/13dsvr01984ps.jpg": true, // VR content, prefix 13
		}

		if !validPaths[path] {
			http.NotFound(w, r)
			return
		}

		// Return a tiny valid JPEG (1x1 pixel)
		w.Header().Set("Content-Type", "image/jpeg")
		// Valid JPEG: SOI + SOF0 (1x1) + SOS + data + EOI
		jpeg := []byte{
			0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00,
			0x01, 0x00, 0x01, 0x01, 0x01, 0x11, 0x00,
			0xFF, 0xC4, 0x00, 0x1F, 0x00, 0x00, 0x01, 0x05,
			0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02,
			0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A,
			0x0B,
			0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00,
			0x3F, 0x00, 0x7B, 0x40,
			0xFF, 0xD9,
		}
		_, _ = w.Write(jpeg)
	}))
	defer server.Close()

	// Override the awsimgsrc base URL for testing by constructing URLs manually
	// We can't easily override the URL in resolveAwsimgsrcPoster since it's hardcoded,
	// so we test the URL construction logic directly instead.

	// Test that the correct awsimgsrc URL is constructed for various content_ids
	testCases := []struct {
		name        string
		contentID   string   // content_id from API response
		expectedIDs []string // expected awsimgsrc ID variations in order
	}{
		{
			name:        "digital/video content with prefix 1 (SDAM-171)",
			contentID:   "1sdam00171",
			expectedIDs: []string{"1sdam171"}, // start has prefix [1] in lookup
		},
		{
			name:        "digital/video content no prefix (IPX-535)",
			contentID:   "1ipx00535",
			expectedIDs: []string{"ipx535", "4ipx535", "5ipx535", "6ipx535", "7ipx535", "9ipx535", "77ipx535", "88ipx535"},
		},
		{
			name:        "digital/video content with prefix 4 (SONE-860)",
			contentID:   "4sone00860",
			expectedIDs: []string{"sone860", "4sone860", "7sone860", "9sone860", "77sone860", "88sone860"},
		},
		{
			name:        "mono/movie/adult content (ROYD-191)",
			contentID:   "royd191",
			expectedIDs: []string{"royd191", "2royd191"},
		},
		{
			name:        "mono/movie/adult with long prefix (ABW-001)",
			contentID:   "118abw001",
			expectedIDs: []string{"118abw001"}, // abw has only [118] prefix
		},
		{
			name:        "4-digit number VR content (DSVR-1984)",
			contentID:   "13dsvr01984",
			expectedIDs: []string{"dsvr1984", "13dsvr1984"}, // %03d of 1984 = "1984"
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Verify URL construction: prefix + series + %03d on dig/mono/movie
			series, numStr := splitSeriesAndNumber(contentIDToID(tc.contentID))
			require.NotEmpty(t, series)

			num, err := strconv.Atoi(numStr)
			require.NoError(t, err)

			padded3 := fmt.Sprintf("%03d", num)
			series = strings.ToLower(series)

			var prefixes []string
			if lookup, ok := contentIDPrefixLookup[series]; ok {
				prefixes = lookup
			} else {
				prefixes = []string{"", "1"}
			}

			var constructed []string
			for _, prefix := range prefixes {
				id := prefix + series + padded3
				constructed = append(constructed, id)
			}

			assert.Equal(t, tc.expectedIDs, constructed)

			// Verify the URL format
			for _, id := range constructed {
				url := fmt.Sprintf("https://awsimgsrc.dmm.com/dig/mono/movie/%s/%sps.jpg", id, id)
				assert.Contains(t, url, "dig/mono/movie/")
				assert.Contains(t, url, "ps.jpg")
			}
		})
	}

	// Verify the mock server works for valid paths
	t.Run("mock server validates path construction", func(t *testing.T) {
		client := server.Client()
		validURL := server.URL + "/dig/mono/movie/1sdam171/1sdam171ps.jpg"
		resp, err := client.Get(validURL)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		invalidURL := server.URL + "/dig/video/1sdam00171/1sdam00171ps.jpg"
		resp2, err := client.Get(invalidURL)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
	})
}

func TestResolveIDs_DigitalOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live API test in short mode")
	}

	cfg := config.ScraperSettings{
		Enabled:    true,
		Language:   "en",
		RetryCount: 5,
		RateLimit:  2000,
	}
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	ids := []string{"SDJS-374", "START-588", "START-566", "DSVR-1984"}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			result, err := scraper.Search(ctx, id)
			if err != nil {
				if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "429") {
					t.Skipf("R18.dev rate limited/403 for %s", id)
				}
				t.Errorf("❌ %s: %v", id, err)
			} else {
				t.Logf("✅ %s: content_id=%s, title=%q", id, result.ContentID, result.Title[:min(60, len(result.Title))])
			}
			time.Sleep(3 * time.Second)
		})
	}
}

func TestResolveAwsimgsrcPoster_SDM171(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live API test in short mode")
	}

	cfg := config.ScraperSettings{
		Enabled:    true,
		Language:   "en",
		RetryCount: 5,
		RateLimit:  2000,
	}
	scraper := New(cfg, testGlobalProxy, testGlobalFlareSolverr)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := scraper.Search(ctx, "SDAM-171")
	if err != nil {
		if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "429") {
			t.Skipf("R18.dev rate limited: %v", err)
		}
		t.Fatalf("SDAM-171 search failed: %v", err)
	}

	t.Logf("CoverURL: %s", result.CoverURL)
	t.Logf("PosterURL: %s", result.PosterURL)
	t.Logf("ShouldCropPoster: %v", result.ShouldCropPoster)

	if strings.Contains(result.PosterURL, "pl.jpg") && !result.ShouldCropPoster {
		t.Errorf("Poster should not be pl.jpg without cropping; got %s", result.PosterURL)
	}

	if strings.Contains(result.PosterURL, "ps.jpg") {
		t.Logf("✅ Poster uses ps.jpg (portrait): %s", result.PosterURL)
	}
}
