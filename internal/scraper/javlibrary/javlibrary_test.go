package javlibrary_test

import (
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraper/javlibrary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		javConfig   config.JavLibraryConfig
		proxyConfig *config.ProxyConfig
		wantError   bool
	}{
		{
			name: "basic scraper",
			javConfig: config.JavLibraryConfig{
				Enabled:         false,
				Language:        "en",
				RequestDelay:    1000,
				BaseURL:         "http://www.javlibrary.com",
				UseFlareSolverr: false,
			},
			proxyConfig: &config.ProxyConfig{},
			wantError:   false,
		},
		{
			name: "scraper with FlareSolverr enabled",
			javConfig: config.JavLibraryConfig{
				Enabled:         false,
				Language:        "en",
				RequestDelay:    1000,
				BaseURL:         "http://www.javlibrary.com",
				UseFlareSolverr: true,
			},
			proxyConfig: &config.ProxyConfig{
				FlareSolverr: config.FlareSolverrConfig{
					Enabled:    true,
					URL:        "http://localhost:8191/v1",
					Timeout:    30,
					MaxRetries: 3,
					SessionTTL: 300,
				},
			},
			wantError: false,
		},
		{
			name: "scraper disabled",
			javConfig: config.JavLibraryConfig{
				Enabled:         false,
				Language:        "en",
				RequestDelay:    1000,
				BaseURL:         "http://www.javlibrary.com",
				UseFlareSolverr: false,
			},
			proxyConfig: &config.ProxyConfig{},
			wantError:   false,
		},
		{
			name: "default language when empty",
			javConfig: config.JavLibraryConfig{
				Enabled:         false,
				Language:        "",
				BaseURL:         "http://www.javlibrary.com",
				UseFlareSolverr: false,
			},
			proxyConfig: &config.ProxyConfig{},
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scraper, err := javlibrary.New(&tt.javConfig, tt.proxyConfig)

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, scraper)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, scraper)
				assert.Equal(t, "javlibrary", scraper.Name())
				assert.Equal(t, tt.javConfig.Enabled, scraper.IsEnabled())
			}
		})
	}
}

func TestScraper_GetURL(t *testing.T) {
	cfg := config.JavLibraryConfig{
		Enabled:         false,
		Language:        "en",
		RequestDelay:    1000,
		BaseURL:         "http://www.javlibrary.com",
		UseFlareSolverr: false,
	}

	scraper, err := javlibrary.New(&cfg, &config.ProxyConfig{})
	require.NoError(t, err)

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
			cfg := config.JavLibraryConfig{
				Enabled:  false,
				Language: tt.language,
				BaseURL:  "http://www.javlibrary.com",
			}

			scraper, err := javlibrary.New(&cfg, &config.ProxyConfig{})
			require.NoError(t, err)

			url, err := scraper.GetURL("IPX-123")
			require.NoError(t, err)
			assert.Contains(t, url, tt.wantPath)
			assert.Contains(t, url, "keyword=IPX-123")
		})
	}
}

func TestScraper_InvalidLanguage(t *testing.T) {
	tests := []struct {
		name     string
		language string
	}{
		{"Korean", "ko"},
		{"French", "fr"},
		{"invalid code", "xx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.JavLibraryConfig{
				Enabled:  false,
				Language: tt.language,
				BaseURL:  "http://www.javlibrary.com",
			}

			_, err := javlibrary.New(&cfg, &config.ProxyConfig{})
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "unsupported")
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
			cfg := config.JavLibraryConfig{
				Enabled:         tt.enabled,
				Language:        "en",
				RequestDelay:    1000,
				BaseURL:         "http://www.javlibrary.com",
				UseFlareSolverr: false,
			}

			scraper, err := javlibrary.New(&cfg, &config.ProxyConfig{})
			require.NoError(t, err)
			assert.Equal(t, tt.enabled, scraper.IsEnabled())
		})
	}
}

// TestScraper_SearchDisabled verifies that Search returns an error when disabled
func TestScraper_SearchDisabled(t *testing.T) {
	cfg := config.JavLibraryConfig{
		Enabled:  false,
		Language: "en",
		BaseURL:  "http://www.javlibrary.com",
	}

	scraper, err := javlibrary.New(&cfg, &config.ProxyConfig{})
	require.NoError(t, err)

	_, err = scraper.Search("IPX-123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// Integration test that requires a running FlareSolverr instance
// Run with: go test -v -timeout 120s ./internal/scraper/javlibrary/... -run TestIntegration_Search
func TestIntegration_Search(t *testing.T) {
	requireJavLibraryIntegration(t)

	cfg := config.JavLibraryConfig{
		Enabled:         true,
		Language:        "en",
		RequestDelay:    1000,
		BaseURL:         "http://www.javlibrary.com",
		UseFlareSolverr: true,
	}

	proxyCfg := &config.ProxyConfig{
		FlareSolverr: config.FlareSolverrConfig{
			Enabled:    true,
			URL:        "http://localhost:8191/v1",
			Timeout:    90,
			MaxRetries: 2,
			SessionTTL: 300,
		},
	}

	scraper, err := javlibrary.New(&cfg, proxyCfg)
	require.NoError(t, err)

	result, err := scraper.Search("IPX-123")
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
