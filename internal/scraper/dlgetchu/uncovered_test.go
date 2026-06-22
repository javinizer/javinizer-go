package dlgetchu

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

func TestScraper_CanHandleURL_Uncovered(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"dl.getchu.com", "http://dl.getchu.com/i/item123456", true},
		{"getchu.com", "http://getchu.com/i/item123456", true},
		{"subdomain", "http://www.dl.getchu.com/i/item123456", true},
		{"unrelated", "https://example.com/item123456", false},
		{"invalid URL", "://not-a-url", false},
		{"empty URL", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

func TestScraper_ExtractIDFromURL_Uncovered(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})

	tests := []struct {
		name        string
		url         string
		expectedID  string
		expectError bool
	}{
		{"item ID in query", "http://dl.getchu.com/index.php?action=article&id=1234567", "1234567", false},
		{"item in path", "http://dl.getchu.com/i/item1234567", "1234567", false},
		{"item with 作品ID prefix", "http://dl.getchu.com/page?作品ID：1234567", "1234567", false},
		{"no extractable ID", "http://example.com/nothing", "", true},
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

func TestExtractNumericID_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"id=1234567", "1234567"},
		{"作品ID：1234567", "1234567"},
		{"/item1234567", "1234567"},
		{"no match", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractNumericID(tt.input))
		})
	}
}

func TestNormalizeFullWidthDigits_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"１２３", "123"},
		{"０９８", "098"},
		{"abc", "abc"},
		{"", ""},
		{"１00", "100"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeFullWidthDigits(tt.input))
		})
	}
}

func TestStripTags_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"<b>bold</b>", "bold"},
		{"<a href=\"/x\">link</a>", "link"},
		{"no tags", "no tags"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripTags(tt.input))
		})
	}
}
