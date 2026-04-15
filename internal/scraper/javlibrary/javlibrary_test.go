package javlibrary_test

import (
	"context"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/javlibrary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanHandleURL(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  false,
		Language: "en",
		BaseURL:  "http://www.javlibrary.com",
	}
	s := javlibrary.New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"javlibrary.com", "https://www.javlibrary.com/en/?v=javli123", true},
		{"with query param", "http://www.javlibrary.com/?v=javli123", true},
		{"other site", "https://www.example.com/ABC-123", false},
		{"malformed URL", "not-a-url", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.CanHandleURL(tt.url)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestExtractIDFromURL(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  false,
		Language: "en",
		BaseURL:  "http://www.javlibrary.com",
	}
	s := javlibrary.New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	tests := []struct {
		name     string
		url      string
		expected string
		wantErr  bool
	}{
		{"query param v", "https://www.javlibrary.com/en/?v=javli123", "javli123", false},
		{"path with v", "https://www.javlibrary.com/?v=javli456", "javli456", false},
		{"empty path", "https://www.javlibrary.com/", "", true},
		{"short segment", "https://www.javlibrary.com/abc", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ExtractIDFromURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestScraperInterfaceCompliance(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  false,
		Language: "en",
		BaseURL:  "http://www.javlibrary.com",
	}
	s := javlibrary.New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})
	var _ models.Scraper = s
	var _ models.URLHandler = s
	var _ models.DirectURLScraper = s
}

func requireJavLibraryIntegration(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test")
	}

	if os.Getenv("JAVINIZER_RUN_FLARESOLVERR_TESTS") != "1" {
		t.Skip("set JAVINIZER_RUN_FLARESOLVERR_TESTS=1 to run JavLibrary integration tests")
	}
}

func TestNewScraper(t *testing.T) {
	tests := []struct {
		name        string
		settings    config.ScraperSettings
		proxyCfg    *config.ProxyConfig
		wantEnabled bool
	}{
		{
			name: "basic scraper",
			settings: config.ScraperSettings{
				Enabled:   false,
				Language:  "en",
				RateLimit: 1000,
				BaseURL:   "http://www.javlibrary.com",
			},
			proxyCfg:    &config.ProxyConfig{},
			wantEnabled: false,
		},
		{
			name: "scraper with FlareSolverr enabled",
			settings: config.ScraperSettings{
				Enabled:         false,
				Language:        "en",
				RateLimit:       1000,
				BaseURL:         "http://www.javlibrary.com",
				UseFlareSolverr: true,
			},
			proxyCfg:    &config.ProxyConfig{},
			wantEnabled: false,
		},
		{
			name: "scraper disabled",
			settings: config.ScraperSettings{
				Enabled:   false,
				Language:  "en",
				RateLimit: 1000,
				BaseURL:   "http://www.javlibrary.com",
			},
			proxyCfg:    &config.ProxyConfig{},
			wantEnabled: false,
		},
		{
			name: "default language when empty",
			settings: config.ScraperSettings{
				Enabled:  false,
				Language: "",
				BaseURL:  "http://www.javlibrary.com",
			},
			proxyCfg:    &config.ProxyConfig{},
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scraper := javlibrary.New(tt.settings, tt.proxyCfg, config.FlareSolverrConfig{})

			assert.NotNil(t, scraper)
			assert.Equal(t, "javlibrary", scraper.Name())
			assert.Equal(t, tt.wantEnabled, scraper.IsEnabled())
		})
	}
}

func TestScraper_GetURL(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:   false,
		Language:  "en",
		RateLimit: 1000,
		BaseURL:   "http://www.javlibrary.com",
	}

	scraper := javlibrary.New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	tests := []struct {
		name string
		id   string
		want string
	}{
		{
			name: "standard ID",
			id:   "IPX-123",
			want: "http://www.javlibrary.com/en/vl_searchbyid.php?keyword=IPX-123",
		},
		{
			name: "ID with letters",
			id:   "SSIS-456",
			want: "http://www.javlibrary.com/en/vl_searchbyid.php?keyword=SSIS-456",
		},
		{
			name: "numeric ID",
			id:   "123456",
			want: "http://www.javlibrary.com/en/vl_searchbyid.php?keyword=123456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := scraper.GetURL(tt.id)
			require.NoError(t, err)
			assert.Equal(t, tt.want, url)
		})
	}
}

func TestScraper_GetURL_Languages(t *testing.T) {
	tests := []struct {
		name     string
		language string
		wantPath string
	}{
		{"English", "en", "/en/"},
		{"Japanese", "ja", "/ja/"},
		{"Chinese Simplified", "cn", "/cn/"},
		{"Chinese Traditional", "tw", "/tw/"},
		{"empty defaults to en", "", "/en/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := config.ScraperSettings{
				Enabled:  false,
				Language: tt.language,
				BaseURL:  "http://www.javlibrary.com",
			}

			scraper := javlibrary.New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

			url, err := scraper.GetURL("IPX-123")
			require.NoError(t, err)
			assert.Contains(t, url, tt.wantPath)
			assert.Contains(t, url, "keyword=IPX-123")
		})
	}
}

func TestScraper_LanguageNormalization(t *testing.T) {
	tests := []struct {
		name     string
		language string
		wantLang string
	}{
		{"Korean (invalid, normalize to en)", "ko", "en"},
		{"French (invalid, normalize to en)", "fr", "en"},
		{"invalid code (normalize to en)", "xx", "en"},
		{"Chinese Simplified (valid)", "cn", "cn"},
		{"Chinese Traditional (valid)", "tw", "tw"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := config.ScraperSettings{
				Enabled:  false,
				Language: tt.language,
				BaseURL:  "http://www.javlibrary.com",
			}

			scraper := javlibrary.New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})
			assert.Equal(t, tt.wantLang, scraper.GetLanguage())
		})
	}
}

func TestScraper_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"enabled", true},
		{"disabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := config.ScraperSettings{
				Enabled:   tt.enabled,
				Language:  "en",
				RateLimit: 1000,
				BaseURL:   "http://www.javlibrary.com",
			}

			scraper := javlibrary.New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})
			assert.Equal(t, tt.enabled, scraper.IsEnabled())
		})
	}
}

// TestScraper_SearchDisabled verifies that Search returns an error when disabled
func TestScraper_SearchDisabled(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  false,
		Language: "en",
		BaseURL:  "http://www.javlibrary.com",
	}

	scraper := javlibrary.New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	_, err := scraper.Search(context.Background(), "IPX-123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// Integration test that requires a running FlareSolverr instance
// Run with: go test -v -timeout 120s ./internal/scraper/javlibrary/... -run TestIntegration_Search
func TestIntegration_Search(t *testing.T) {
	requireJavLibraryIntegration(t)

	settings := config.ScraperSettings{
		Enabled:         true,
		Language:        "en",
		RateLimit:       1000,
		BaseURL:         "http://www.javlibrary.com",
		UseFlareSolverr: true,
	}

	proxyCfg := &config.ProxyConfig{}

	scraper := javlibrary.New(settings, proxyCfg, config.FlareSolverrConfig{})

	result, err := scraper.Search(context.Background(), "IPX-123")
	if err != nil {
		t.Skipf("FlareSolverr may not be running: %v", err)
	}

	assert.NotNil(t, result)
	assert.Equal(t, "javlibrary", result.Source)
	assert.Equal(t, "IPX-123", result.ID)
	assert.NotEmpty(t, result.Title)
	assert.NotEmpty(t, result.CoverURL)
	assert.NotNil(t, result.ReleaseDate)
	assert.Greater(t, result.Runtime, 0)
	assert.NotEmpty(t, result.Maker)
	assert.NotEmpty(t, result.Genres)

	t.Logf("Title: %s", result.Title)
	t.Logf("Cover: %s", result.CoverURL)
	t.Logf("Director: %s", result.Director)
	t.Logf("Maker: %s", result.Maker)
	t.Logf("Label: %s", result.Label)
	t.Logf("Runtime: %d min", result.Runtime)
	t.Logf("Release: %s", result.ReleaseDate.Format("2006-01-02"))
	t.Logf("Genres: %v", result.Genres)
	t.Logf("Actresses: %+v", result.Actresses)
}
