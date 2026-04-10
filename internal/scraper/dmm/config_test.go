package dmm

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.ScraperSettings
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config returns error",
			cfg:     nil,
			wantErr: true,
			errMsg:  "dmm: config is nil",
		},
		{
			name: "disabled scraper is valid",
			cfg: &config.ScraperSettings{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "RateLimit -1 is invalid",
			cfg: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: -1,
			},
			wantErr: true,
			errMsg:  "dmm: rate_limit must be non-negative, got -1",
		},
		{
			name: "RateLimit 0 is valid",
			cfg: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 0,
			},
			wantErr: false,
		},
		{
			name: "RateLimit 1000 is valid",
			cfg: &config.ScraperSettings{
				Enabled:   true,
				RateLimit: 1000,
			},
			wantErr: false,
		},
		{
			name: "RetryCount -1 is invalid",
			cfg: &config.ScraperSettings{
				Enabled:    true,
				RetryCount: -1,
			},
			wantErr: true,
			errMsg:  "dmm: retry_count must be non-negative, got -1",
		},
		{
			name: "RetryCount 0 is valid",
			cfg: &config.ScraperSettings{
				Enabled:    true,
				RetryCount: 0,
			},
			wantErr: false,
		},
		{
			name: "RetryCount 3 is valid",
			cfg: &config.ScraperSettings{
				Enabled:    true,
				RetryCount: 3,
			},
			wantErr: false,
		},
		{
			name: "Timeout -1 is invalid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				Timeout: -1,
			},
			wantErr: true,
			errMsg:  "dmm: timeout must be non-negative, got -1",
		},
		{
			name: "Timeout 0 is valid",
			cfg: &config.ScraperSettings{
				Enabled: true,
				Timeout: 0,
			},
			wantErr: false,
		},
		{
			name: "basic valid config",
			cfg: &config.ScraperSettings{
				Enabled: true,
				Timeout: 30,
			},
			wantErr: false,
		},
		{
			name: "all valid fields",
			cfg: &config.ScraperSettings{
				Enabled:    true,
				RateLimit:  500,
				RetryCount: 3,
				Timeout:    60,
			},
			wantErr: false,
		},
	}

	c := &DMMConfig{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.ValidateConfig(tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Equal(t, tt.errMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDMMConfig_ToScraperSettings(t *testing.T) {
	testHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cfg := &DMMConfig{
		Enabled:                true,
		RequestDelay:           500,
		MaxRetries:             7,
		UserAgent:              "CustomUA/1.0",
		UseBrowser:             true,
		ScrapeActress:          true,
		PlaceholderThresholdKB: 15,
		ExtraPlaceholderHashes: []string{testHash},
	}

	settings := cfg.ToScraperSettings()

	assert.True(t, settings.Enabled, "Enabled should be preserved")
	assert.Equal(t, 500, settings.RateLimit, "RateLimit (from RequestDelay) should be preserved")
	assert.Equal(t, 7, settings.RetryCount, "RetryCount (from MaxRetries) should be preserved")
	assert.Equal(t, "CustomUA/1.0", settings.UserAgent, "UserAgent should be preserved")
	assert.True(t, settings.UseBrowser, "UseBrowser should be preserved")
	assert.NotNil(t, settings.ScrapeActress, "ScrapeActress should be set")
	assert.True(t, *settings.ScrapeActress, "ScrapeActress should be true")
	assert.NotNil(t, settings.Extra, "Extra should be initialized")
	assert.Equal(t, 15, settings.Extra["placeholder_threshold"], "Placeholder threshold should be in Extra")
	assert.NotNil(t, settings.Extra["extra_placeholder_hashes"], "Placeholder hashes should be in Extra")
}

func TestDMMConfig_ToScraperSettings_ScrapeActressFalse(t *testing.T) {
	cfg := &DMMConfig{
		Enabled:       true,
		ScrapeActress: false,
	}

	settings := cfg.ToScraperSettings()

	assert.NotNil(t, settings.ScrapeActress, "ScrapeActress should be non-nil even when false")
	assert.False(t, *settings.ScrapeActress, "ScrapeActress should preserve explicit false value")
}

func TestDMMConfig_ToScraperSettings_EmptyFields(t *testing.T) {
	cfg := &DMMConfig{
		Enabled: true,
	}

	settings := cfg.ToScraperSettings()

	assert.True(t, settings.Enabled)
	assert.Equal(t, 0, settings.RateLimit)
	assert.Equal(t, 0, settings.RetryCount)
	assert.Empty(t, settings.UserAgent)
	assert.False(t, settings.UseBrowser)
	assert.NotNil(t, settings.ScrapeActress, "ScrapeActress should be non-nil")
	assert.False(t, *settings.ScrapeActress, "ScrapeActress should be false when not set")
	assert.NotNil(t, settings.Extra)
	assert.Len(t, settings.Extra, 0, "Extra should be empty when no placeholder fields set")
}
