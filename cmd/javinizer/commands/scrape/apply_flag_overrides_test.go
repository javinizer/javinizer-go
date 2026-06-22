package scrape

import (
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Register scraper defaults for Finalize
	_ "github.com/javinizer/javinizer-go/internal/scraper/dmm"
)

// TestApplyFlagOverrides_ActressDB tests actress-db flag overrides
func TestApplyFlagOverrides_ActressDB(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		flagVal  bool
		expected bool
	}{
		{"enable actress-db", "actress-db", true, true},
		{"disable no-actress-db", "no-actress-db", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCommand()
			cfg := config.DefaultConfig(nil, nil)
			cfg.Metadata.ActressDatabase.Enabled = !tt.expected

			err := cmd.Flags().Set(tt.flagName, fmt.Sprintf("%t", tt.flagVal))
			require.NoError(t, err)

			ApplyFlagOverrides(cmd, cfg)

			assert.Equal(t, tt.expected, cfg.Metadata.ActressDatabase.Enabled,
				"Flag %s should set ActressDatabase.Enabled to %t", tt.flagName, tt.expected)
		})
	}
}

// TestApplyFlagOverrides_GenreReplacement tests genre-replacement flag overrides
func TestApplyFlagOverrides_GenreReplacement(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		flagVal  bool
		expected bool
	}{
		{"enable genre-replacement", "genre-replacement", true, true},
		{"disable no-genre-replacement", "no-genre-replacement", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCommand()
			cfg := config.DefaultConfig(nil, nil)
			cfg.Metadata.GenreReplacement.Enabled = !tt.expected

			err := cmd.Flags().Set(tt.flagName, fmt.Sprintf("%t", tt.flagVal))
			require.NoError(t, err)

			ApplyFlagOverrides(cmd, cfg)

			assert.Equal(t, tt.expected, cfg.Metadata.GenreReplacement.Enabled,
				"Flag %s should set GenreReplacement.Enabled to %t", tt.flagName, tt.expected)
		})
	}
}

// DMM-specific flag tests (scrape-actress, browser, browser-timeout) are restored
// now that ScraperSettings has the required fields (ScrapeActress *bool, UseBrowser bool).

func TestApplyFlagOverrides_ScrapeActress(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		flagVal  bool
		expected bool
	}{
		{"enable scrape-actress", "scrape-actress", true, true},
		{"disable no-scrape-actress", "no-scrape-actress", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCommand()
			cfg := config.DefaultConfig(nil, nil)

			err := cmd.Flags().Set(tt.flagName, fmt.Sprintf("%t", tt.flagVal))
			require.NoError(t, err)

			ApplyFlagOverrides(cmd, cfg)

			require.NotNil(t, cfg.Scrapers.Overrides["dmm"], "dmm override should exist")
			require.NotNil(t, cfg.Scrapers.Overrides["dmm"].ScrapeActress, "ScrapeActress should be set")
			assert.Equal(t, tt.expected, *cfg.Scrapers.Overrides["dmm"].ScrapeActress)
		})
	}
}

func TestApplyFlagOverrides_Browser(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		flagVal  bool
		expected bool
	}{
		{"enable browser", "browser", true, true},
		{"disable no-browser", "no-browser", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCommand()
			cfg := config.DefaultConfig(nil, nil)

			err := cmd.Flags().Set(tt.flagName, fmt.Sprintf("%t", tt.flagVal))
			require.NoError(t, err)

			ApplyFlagOverrides(cmd, cfg)

			require.NotNil(t, cfg.Scrapers.Overrides["dmm"], "dmm override should exist")
			assert.Equal(t, tt.expected, cfg.Scrapers.Overrides["dmm"].UseBrowser)
		})
	}
}

func TestApplyFlagOverrides_BrowserTimeout(t *testing.T) {
	t.Run("positive timeout sets value", func(t *testing.T) {
		cmd := NewCommand()
		cfg := config.DefaultConfig(nil, nil)

		err := cmd.Flags().Set("browser-timeout", "60")
		require.NoError(t, err)

		ApplyFlagOverrides(cmd, cfg)

		assert.Equal(t, 60, cfg.Scrapers.Browser.Timeout)
	})

	t.Run("zero timeout does not override", func(t *testing.T) {
		cmd := NewCommand()
		cfg := config.DefaultConfig(nil, nil)
		cfg.Scrapers.Browser.Timeout = 30

		err := cmd.Flags().Set("browser-timeout", "0")
		require.NoError(t, err)

		ApplyFlagOverrides(cmd, cfg)

		assert.Equal(t, 30, cfg.Scrapers.Browser.Timeout, "zero timeout should not override existing value")
	})
}

func TestApplyFlagOverrides_ScrapeActressNoFlagUnchanged(t *testing.T) {
	cmd := NewCommand()
	cfg := config.DefaultConfig(nil, nil)

	ApplyFlagOverrides(cmd, cfg)

	// Without setting any DMM flags, the dmm override should not be created by CLI flags alone
	assert.Nil(t, cfg.Scrapers.Overrides["dmm"])
}
