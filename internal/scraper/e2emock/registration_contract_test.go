package e2emock_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/e2emock"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestRegisteredMockAcceptsSettingsWithoutBypassingGenericValidation(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	e2emock.Register(registry)
	settings := &models.ScraperSettings{Enabled: true, Language: " EN "}
	original := settings.Clone()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{e2emock.Name: settings}
	e2emock.ApplyToConfig(cfg)
	require.NoError(t, cfg.Scrapers.Finalize(registry))
	require.Equal(t, []string{e2emock.Name}, cfg.Scrapers.Priority)
	resolved := cfg.Scrapers.ResolvedSettings(e2emock.Name)
	require.True(t, resolved.Enabled)
	require.Equal(t, original.Language, resolved.Language)
	// This invokes registered module validation through configuration, not by
	// directly calling the callback. Validation normalizes a resolved copy.
	require.NoError(t, config.ValidateScraperOverrides(cfg))
	require.Equal(t, original, *settings)
	require.NotNil(t, registry.GetValidateFn(e2emock.Name))
	settings.RateLimit = -1
	require.ErrorContains(t, config.ValidateScraperOverrides(cfg), "e2emock: rate_limit must be non-negative")
	require.Equal(t, -1, settings.RateLimit)
	require.Equal(t, original.Language, settings.Language)
}
