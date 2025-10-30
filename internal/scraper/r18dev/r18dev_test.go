package r18dev

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
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

// createTestConfig creates a test configuration
func createTestConfig(enabled bool) *config.Config {
	return &config.Config{
		Scrapers: config.ScrapersConfig{
			UserAgent: "Test Agent",
			R18Dev: config.R18DevConfig{
				Enabled: enabled,
			},
			Proxy: config.ProxyConfig{
				Enabled: false,
			},
		},
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		enabled bool
	}{
		{
			name:    "Enabled scraper",
			cfg:     createTestConfig(true),
			enabled: true,
		},
		{
			name:    "Disabled scraper",
			cfg:     createTestConfig(false),
			enabled: false,
		},
		{
			name: "Custom user agent",
			cfg: &config.Config{
				Scrapers: config.ScrapersConfig{
					UserAgent: "Custom Agent",
					R18Dev: config.R18DevConfig{
						Enabled: true,
					},
					Proxy: config.ProxyConfig{
						Enabled: false,
					},
				},
			},
			enabled: true,
		},
		{
			name: "With proxy configuration",
			cfg: &config.Config{
				Scrapers: config.ScrapersConfig{
					UserAgent: "Test Agent",
					R18Dev: config.R18DevConfig{
						Enabled: true,
					},
					Proxy: config.ProxyConfig{
						Enabled:  true,
						URL:      "http://proxy.example.com:8080",
						Username: "user",
						Password: "pass",
					},
				},
			},
			enabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scraper := New(tt.cfg)
			assert.NotNil(t, scraper)
			assert.NotNil(t, scraper.client)
			assert.Equal(t, tt.enabled, scraper.enabled)
		})
	}
}

func TestScraper_Name(t *testing.T) {
	cfg := createTestConfig(true)
	scraper := New(cfg)
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
			cfg := createTestConfig(tt.enabled)
			scraper := New(cfg)
			assert.Equal(t, tt.enabled, scraper.IsEnabled())
		})
	}
}

func TestScraper_GetURL(t *testing.T) {
	cfg := createTestConfig(true)
	scraper := New(cfg)

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
			w.Write(loadTestData(t, "ipx535_dvdid_response.json"))
			return
		}

		if r.URL.Path == "/videos/vod/movies/detail/-/combined=1ipx00535/json" ||
			r.URL.Path == "/videos/vod/movies/detail/-/combined=ipx535/json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(loadTestData(t, "ipx535_full_response.json"))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create scraper with test server URL
	cfg := createTestConfig(true)
	scraper := New(cfg)

	// Override base URL for testing (we need to modify the client to use test server)
	// For now, we'll test the parsing logic separately since we can't easily override the URL

	// Test parseResponse directly instead
	var data R18Response
	err := json.Unmarshal(loadTestData(t, "ipx535_full_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(&data, "https://r18.dev/test")
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
	cfg := createTestConfig(true)
	scraper := New(cfg)

	var data R18Response
	err := json.Unmarshal(loadTestData(t, "legacy_format_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(&data, "https://r18.dev/test")
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
	cfg := createTestConfig(true)
	scraper := New(cfg)

	var data R18Response
	err := json.Unmarshal(loadTestData(t, "minimal_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(&data, "https://r18.dev/test")
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
	cfg := createTestConfig(true)
	scraper := New(cfg)

	var data R18Response
	err := json.Unmarshal(loadTestData(t, "empty_arrays_response.json"), &data)
	require.NoError(t, err)

	result, err := scraper.parseResponse(&data, "https://r18.dev/test")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify empty arrays don't cause nil panics
	// Note: parseResponse initializes slices with make(), so they're never nil
	assert.Empty(t, result.Actresses)
	assert.Empty(t, result.Genres)
	assert.Empty(t, result.ScreenshotURL)
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
			expected: "helloworld",
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
			result := cleanString(tt.input)
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
	cfg := createTestConfig(true)
	scraper := New(cfg)

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
				Title:     "Test",
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

			result, err := scraper.parseResponse(data, "https://r18.dev/test")
			require.NoError(t, err)
			require.Len(t, result.Actresses, 1)

			assert.Contains(t, result.Actresses[0].ThumbURL, tt.expectedSuffix)
		})
	}
}

func TestInvalidDateParsing(t *testing.T) {
	cfg := createTestConfig(true)
	scraper := New(cfg)

	data := &R18Response{
		DVDID:       "TEST-001",
		ContentID:   "test00001",
		Title:       "Test",
		ReleaseDate: "invalid-date-format",
	}

	result, err := scraper.parseResponse(data, "https://r18.dev/test")
	require.NoError(t, err)

	// Invalid date should result in nil ReleaseDate, not an error
	assert.Nil(t, result.ReleaseDate)
}

func TestFallbackBehavior(t *testing.T) {
	cfg := createTestConfig(true)
	scraper := New(cfg)

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
				Title:     "Test",
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
				Title:     "Test",
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
				Title:     "Test",
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
				Title:      "Test",
				SeriesName: "Fallback Series",
			},
			checkField:  "series",
			expectedVal: "Fallback Series",
			description: "Should try SeriesNameEn, then Series.Name, then SeriesName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(tt.data, "https://r18.dev/test")
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
	cfg := createTestConfig(true)
	scraper := New(cfg)

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
				Title:          "Test",
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
				Title:     "Test",
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
				Title:     "Test",
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
				Title:     "Test",
			},
			expectCover: false,
			description: "Should handle missing images gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(tt.data, "https://r18.dev/test")
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
	cfg := createTestConfig(true)
	scraper := New(cfg)

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
				Title:     "Test",
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
				Title:     "Test",
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
				Title:     "Test",
			},
			expectedCount: 0,
			description:   "Should handle missing screenshots gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(tt.data, "https://r18.dev/test")
			require.NoError(t, err, tt.description)
			assert.Len(t, result.ScreenshotURL, tt.expectedCount, tt.description)
		})
	}
}

func TestTrailerURLFallbacks(t *testing.T) {
	cfg := createTestConfig(true)
	scraper := New(cfg)

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
				Title:     "Test",
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
				Title:     "Test",
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
				Title:     "Test",
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
				Title:     "Test",
			},
			expectURL:   false,
			description: "Should handle missing trailer gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(tt.data, "https://r18.dev/test")
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
	cfg := createTestConfig(true)
	scraper := New(cfg)

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
				Title:        "Test",
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
				Title:     "Test",
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
				Title:      "Test",
				SeriesName: "Fallback Series Name",
			},
			expected: "Fallback Series Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(tt.data, "https://r18.dev/test")
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
	cfg := createTestConfig(true)
	scraper := New(cfg)

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
				Title:     "Test",
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

			result, err := scraper.parseResponse(data, "https://r18.dev/test")
			require.NoError(t, err)
			require.Len(t, result.Actresses, 1)

			assert.Equal(t, tt.expectedFirst, result.Actresses[0].FirstName)
			assert.Equal(t, tt.expectedLast, result.Actresses[0].LastName)
		})
	}
}

func TestIDResolution(t *testing.T) {
	cfg := createTestConfig(true)
	scraper := New(cfg)

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
				Title:     "Test",
			},
			expectedID:  "IPX-535",
			description: "Should prefer DVDID when available",
		},
		{
			name: "ContentID fallback",
			data: &R18Response{
				DVDID:     "",
				ContentID: "1ipx00535",
				Title:     "Test",
			},
			expectedID:  "IPX-535",
			description: "Should convert ContentID when DVDID missing",
		},
		{
			name: "Both empty",
			data: &R18Response{
				DVDID:     "",
				ContentID: "",
				Title:     "Test",
			},
			expectedID:  "",
			description: "Should handle both empty gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scraper.parseResponse(tt.data, "https://r18.dev/test")
			require.NoError(t, err, tt.description)
			assert.Equal(t, tt.expectedID, result.ID, tt.description)
		})
	}
}
