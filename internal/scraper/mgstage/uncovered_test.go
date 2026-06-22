package mgstage

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestScraper_ExtractIDFromURL_Uncovered(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})

	tests := []struct {
		name        string
		url         string
		expectedID  string
		expectError bool
	}{
		{"valid product URL", "https://www.mgstage.com/product/product_detail/ABCD-123/", "ABCD-123", false},
		{"valid product URL no trailing slash", "https://www.mgstage.com/product/product_detail/LUXU-1806", "LUXU-1806", false},
		{"invalid URL", "https://example.com/no-id", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := s.ExtractIDFromURL(tt.url)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, id)
			}
		})
	}
}

func TestExtractIDFromURL_Standalone(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://www.mgstage.com/product/product_detail/ABCD-123/", "ABCD-123"},
		{"https://www.mgstage.com/product/product_detail/luxu-1806/", "LUXU-1806"},
		{"https://example.com/no-id", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractIDFromURL(tt.input))
		})
	}
}

func TestNormalizeIDForSearch(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABCD-123", "abcd123"},
		{"luxu-1806", "luxu1806"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeIDForSearch(tt.input))
		})
	}
}
