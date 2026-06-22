package scraperconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraperSettings_Validate_NilSettings(t *testing.T) {
	var s *ScraperSettings
	err := s.Validate("r18dev")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "r18dev: config is nil")
}

func TestScraperSettings_Validate_DisabledSettings(t *testing.T) {
	s := &ScraperSettings{Enabled: false}
	err := s.Validate("r18dev")
	assert.NoError(t, err)
}

func TestScraperSettings_Validate_NegativeRateLimit(t *testing.T) {
	s := &ScraperSettings{Enabled: true, RateLimit: -1}
	err := s.Validate("r18dev")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit must be non-negative")
}

func TestScraperSettings_Validate_NegativeRetryCount(t *testing.T) {
	s := &ScraperSettings{Enabled: true, RetryCount: -1}
	err := s.Validate("javbus")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "retry_count must be non-negative")
}

func TestScraperSettings_Validate_NegativeTimeout(t *testing.T) {
	s := &ScraperSettings{Enabled: true, Timeout: -1}
	err := s.Validate("dmm")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout must be non-negative")
}

func TestScraperSettings_Validate_ZeroTimeout(t *testing.T) {
	// A timeout of 0 is valid — it means "use the global timeout setting".
	s := &ScraperSettings{Enabled: true, Timeout: 0}
	err := s.Validate("r18dev")
	assert.NoError(t, err)
}

func TestScraperSettings_Validate_ValidTimeout(t *testing.T) {
	s := &ScraperSettings{Enabled: true, Timeout: 1}
	err := s.Validate("r18dev")
	assert.NoError(t, err)
}

func TestScraperSettings_Validate_ValidSettings(t *testing.T) {
	s := &ScraperSettings{Enabled: true, RateLimit: 100, RetryCount: 3, Timeout: 30}
	err := s.Validate("r18dev")
	assert.NoError(t, err)
}

func TestScraperSettings_Validate_ScraperNamePrefix(t *testing.T) {
	testCases := []struct {
		name        string
		scraperName string
		expected    string
	}{
		{"r18dev prefix", "r18dev", "r18dev:"},
		{"javbus prefix", "javbus", "javbus:"},
		{"dmm prefix", "dmm", "dmm:"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var s *ScraperSettings
			err := s.Validate(tc.scraperName)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.expected)
		})
	}
}

func TestValidate_LanguageNormalization(t *testing.T) {
	t.Parallel()

	t.Run("trims and lowercases language", func(t *testing.T) {
		s := &ScraperSettings{Enabled: true, Language: "  JA  "}
		err := s.Validate("test")
		require.NoError(t, err)
		assert.Equal(t, "ja", s.Language)
	})

	t.Run("empty language stays empty", func(t *testing.T) {
		s := &ScraperSettings{Enabled: true, Language: "   "}
		err := s.Validate("test")
		require.NoError(t, err)
		assert.Equal(t, "", s.Language)
	})
}
