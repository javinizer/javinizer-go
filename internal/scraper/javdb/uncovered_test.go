package javdb

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraper_Name(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.Equal(t, "javdb", s.Name())
}

func TestScraper_IsEnabled(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
		assert.True(t, s.IsEnabled())
	})
	t.Run("disabled", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: false}, nil, models.FlareSolverrConfig{})
		assert.False(t, s.IsEnabled())
	})
}

func TestScraper_Config(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, Timeout: 30}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	cfg := s.Config()
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 30, cfg.Timeout)
	// Verify it's a clone (modifying returned config doesn't affect scraper)
	cfg.Enabled = false
	assert.True(t, s.Config().Enabled)
}

func TestScraper_Close(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.NoError(t, s.Close())
	// Calling Close twice should not panic
	assert.NoError(t, s.Close())
}

func TestScraper_CanHandleURL(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"javdb.com detail page", "https://javdb.com/v/abcde", true},
		{"javdb.com search", "https://javdb.com/search?q=ABC-123", true},
		{"subdomain of javdb.com", "https://www.javdb.com/v/abcde", true},
		{"unrelated site", "https://example.com/v/abcde", false},
		{"invalid URL", "://not-a-url", false},
		{"empty URL", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

func TestScraper_ExtractIDFromURL(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})

	tests := []struct {
		name        string
		url         string
		expectedID  string
		expectError bool
	}{
		{"valid detail page", "https://javdb.com/v/abcde", "abcde", false},
		{"valid detail page with query", "https://javdb.com/v/abcde?q=test", "abcde", false},
		{"search page no ID", "https://javdb.com/search?q=ABC-123", "", true},
		{"invalid URL", "://not-a-url", "", true},
		{"unrelated URL no video code", "https://example.com/page/test", "", true},
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

func TestScraper_ResolveDownloadProxyForHost(t *testing.T) {
	dlProxy := &models.ProxyConfig{Enabled: true, Profile: "dl"}
	s := newScraper(&models.ScraperSettings{Enabled: true, DownloadProxy: dlProxy}, nil, models.FlareSolverrConfig{})

	t.Run("javdb host returns proxy", func(t *testing.T) {
		dl, _, ok := s.ResolveDownloadProxyForHost("javdb.com")
		assert.True(t, ok)
		assert.Equal(t, dlProxy, dl)
	})
	t.Run("subdomain of javdb returns proxy", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("cdn.javdb.com")
		assert.True(t, ok)
	})
	t.Run("unrelated host returns false", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("example.com")
		assert.False(t, ok)
	})
	t.Run("empty host returns false", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("")
		assert.False(t, ok)
	})
}

func TestNormalizeIDForCompare(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABC-123", "ABC123"},
		{"abc_123", "ABC123"},
		{"  ABC-123  ", "ABC123"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeIDForCompare(tt.input))
		})
	}
}

func TestTrimVariantSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABC123A", "ABC123"},
		{"ABC123", "ABC123"},
		{"AB", "AB"},       // too short for variant
		{"ABC12", "ABC12"}, // prev char not digit
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, trimVariantSuffix(tt.input))
		})
	}
}

func TestTrimNumericPadding(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABC00123", "ABC123"},
		{"ABC000", "ABC0"},
		{"ABC123", "ABC123"},
		{"NODIGITS", "NODIGITS"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, trimNumericPadding(tt.input))
		})
	}
}

func TestIdsMatch(t *testing.T) {
	assert.True(t, idsMatch("ABC-123", "ABC123"))
	assert.True(t, idsMatch("ABC-123", "abc123"))
	assert.False(t, idsMatch("ABC-123", "XYZ-456"))
	assert.False(t, idsMatch("", "ABC-123"))
	assert.False(t, idsMatch("ABC-123", ""))
}

func TestIdMatchRank(t *testing.T) {
	assert.Equal(t, idMatchExact, idMatchRank("ABC-123", "ABC123"))
	assert.Equal(t, idMatchNormalized, idMatchRank("ABC00123", "ABC123"))
	assert.Equal(t, idMatchNone, idMatchRank("ABC-123", "XYZ-456"))
}

func TestNormalizeLabel(t *testing.T) {
	assert.Equal(t, "cast", normalizeLabel("Cast："))
	assert.Equal(t, "cast", normalizeLabel("Cast:"))
	assert.Equal(t, "cast", normalizeLabel("Cast"))
	assert.Equal(t, "", normalizeLabel(""))
}

func TestLabelContains(t *testing.T) {
	label := normalizeLabel("Male Actor")
	assert.True(t, labelContains(label, "male actor"))
	assert.True(t, labelContains(label, "male"))
	assert.False(t, labelContains(normalizeLabel("Director"), "male actor"))
}

func TestClassifyCastLabel(t *testing.T) {
	assert.Equal(t, castLabelMale, classifyCastLabel("male actor"))
	assert.Equal(t, castLabelFemale, classifyCastLabel("actress(es)"))
	assert.Equal(t, castLabelGeneric, classifyCastLabel("cast"))
	assert.Equal(t, castLabelUnknown, classifyCastLabel("unknown label"))
}

func TestParseRuntime_Uncovered(t *testing.T) {
	assert.Equal(t, 120, parseRuntime("120 min"))
	assert.Equal(t, 90, parseRuntime("90"))
	assert.Equal(t, 0, parseRuntime(""))
	assert.Equal(t, 0, parseRuntime("no runtime"))
}

func TestIsNotAvailableValue(t *testing.T) {
	assert.True(t, isNotAvailableValue("N/A"))
	assert.True(t, isNotAvailableValue("n/a"))
	assert.True(t, isNotAvailableValue("--"))
	assert.True(t, isNotAvailableValue("なし"))
	assert.False(t, isNotAvailableValue(""))
	assert.False(t, isNotAvailableValue("Valid Title"))
}

func TestIsJavDBVideoCode(t *testing.T) {
	tests := []struct {
		id       string
		expected bool
	}{
		{"ABC123", true},
		{"abc123", true},
		{"123ABC", true},
		{"", false},
		{"AB", false},            // too short
		{"ABCDEFGHIJKLM", false}, // too long (>12)
		{"ABC-123", false},       // hyphen not allowed
		{"FC2-1234567", false},   // hyphen not allowed
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			assert.Equal(t, tt.expected, isJavDBVideoCode(tt.id))
		})
	}
}
