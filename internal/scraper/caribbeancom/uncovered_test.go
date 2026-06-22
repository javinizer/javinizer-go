package caribbeancom

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraper_Name_Uncovered(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.Equal(t, "caribbeancom", s.Name())
}

func TestScraper_IsEnabled_Uncovered(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
		assert.True(t, s.IsEnabled())
	})
	t.Run("disabled", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: false}, nil, models.FlareSolverrConfig{})
		assert.False(t, s.IsEnabled())
	})
}

func TestScraper_Config_Uncovered(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, Timeout: 30}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	cfg := s.Config()
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 30, cfg.Timeout)
	// Verify it's a clone
	cfg.Enabled = false
	assert.True(t, s.Config().Enabled)
}

func TestScraper_Close_Uncovered(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.NoError(t, s.Close())
}

func TestIsMovieDetailPage(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{"movie_id JSON present", `<script>var Movie = {"movie_id": "012345-001"};</script>`, true},
		{"null movie", `<script>var Movie = null;</script>`, false},
		{"empty html", ``, false},
		{"movie-info present", `<div class="movie-info">content</div>`, true},
		{"404 page", `<div class="error404-wrap">not found</div>`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			assert.Equal(t, tt.expected, isMovieDetailPage(doc, tt.html))
		})
	}
}
