package caribbeancom

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetURL tests URL generation for Japanese and English
func TestGetURL(t *testing.T) {
	tests := []struct {
		name     string
		language string
		movieID  string
		want     string
		wantErr  bool
	}{
		{
			name:     "Japanese direct ID",
			language: "ja",
			movieID:  "120614-753",
			want:     "https://www.caribbeancom.com/moviepages/120614-753/index.html",
			wantErr:  false,
		},
		{
			name:     "English direct ID",
			language: "en",
			movieID:  "120614-753",
			want:     "https://en.caribbeancom.com/eng/moviepages/120614-753/index.html",
			wantErr:  false,
		},
		{
			name:     "Japanese underscore ID normalized",
			language: "ja",
			movieID:  "120614_753",
			want:     "https://www.caribbeancom.com/moviepages/120614-753/index.html",
			wantErr:  false,
		},
		{
			name:     "English underscore ID normalized",
			language: "en",
			movieID:  "020326_01-10MU",
			want:     "https://en.caribbeancom.com/eng/moviepages/020326-001/index.html",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := config.ScraperSettings{Enabled: true, Language: tt.language}
			scraper := New(settings, nil, config.FlareSolverrConfig{})

			url, err := scraper.GetURL(tt.movieID)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, url)
		})
	}
}

// TestGetURL_InvalidID tests GetURL with invalid movie IDs
func TestGetURL_InvalidID(t *testing.T) {
	settings := config.ScraperSettings{Enabled: true, Language: "ja"}
	scraper := New(settings, nil, config.FlareSolverrConfig{})

	url, err := scraper.GetURL("invalid-id")
	assert.Error(t, err)
	assert.Empty(t, url)
}

// TestGetURL_WithBaseURL tests GetURL with custom base URL
func TestGetURL_WithBaseURL(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:   true,
		Language:  "ja",
		RateLimit: 0,
	}
	// Test with base URL that doesn't have trailing slash
	settings.BaseURL = "https://www.caribbeancom.com"
	scraper := New(settings, nil, config.FlareSolverrConfig{})

	url, err := scraper.GetURL("120614-753")
	require.NoError(t, err)
	// Should handle missing trailing slash
	assert.Equal(t, "https://www.caribbeancom.com/moviepages/120614-753/index.html", url)
}

// TestResolveDownloadProxyForHost tests proxy resolution for Caribbeancom hosts
