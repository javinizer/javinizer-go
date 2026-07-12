package config

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/scraperconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newConfigWithOverrides(overrides map[string]*scraperconfig.ScraperSettings) *Config {
	return &Config{
		Scrapers: ScrapersConfig{
			Overrides: overrides,
		},
		Metadata: MetadataConfig{
			Priority: PriorityConfig{
				Fields: make(map[string][]string),
			},
		},
	}
}

func scraperSettings(enabled bool) *scraperconfig.ScraperSettings {
	return &scraperconfig.ScraperSettings{Enabled: enabled}
}

func TestValidatePriorityOverrides_AllDisabled(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm":    scraperSettings(false),
		"r18dev": scraperSettings(true),
	})
	cfg.Metadata.Priority.Fields["content_id"] = []string{"dmm"}

	warnings := ValidatePriorityOverrides(cfg)
	require.Len(t, warnings, 1)
	assert.Equal(t, "content_id", warnings[0].Field)
	assert.Equal(t, []string{"dmm"}, warnings[0].Scrapers)
	assert.Contains(t, warnings[0].Message, "dmm")
	assert.Contains(t, warnings[0].Message, "disabled")
}

func TestValidatePriorityOverrides_MultiAllDisabled(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm":    scraperSettings(false),
		"javbus": scraperSettings(false),
		"r18dev": scraperSettings(true),
	})
	cfg.Metadata.Priority.Fields["title"] = []string{"dmm", "javbus"}

	warnings := ValidatePriorityOverrides(cfg)
	require.Len(t, warnings, 1)
	assert.Equal(t, "title", warnings[0].Field)
	assert.Contains(t, warnings[0].Scrapers, "dmm")
	assert.Contains(t, warnings[0].Scrapers, "javbus")
}

func TestValidatePriorityOverrides_MultiSomeDisabled(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm":    scraperSettings(false),
		"r18dev": scraperSettings(true),
	})
	cfg.Metadata.Priority.Fields["content_id"] = []string{"dmm", "r18dev"}

	warnings := ValidatePriorityOverrides(cfg)
	assert.Empty(t, warnings, "no warning when at least one scraper is enabled")
}

func TestValidatePriorityOverrides_NoOverrides(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm": scraperSettings(false),
	})

	warnings := ValidatePriorityOverrides(cfg)
	assert.Empty(t, warnings)
}

func TestValidatePriorityOverrides_SkipSentinel(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm": scraperSettings(false),
	})
	cfg.Metadata.Priority.Fields["series"] = []string{"__skip__"}

	warnings := ValidatePriorityOverrides(cfg)
	assert.Empty(t, warnings, "__skip__ should not generate a warning")
}

func TestValidatePriorityOverrides_EmptyOverride(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm": scraperSettings(false),
	})
	cfg.Metadata.Priority.Fields["title"] = []string{}

	warnings := ValidatePriorityOverrides(cfg)
	assert.Empty(t, warnings, "empty override inherits global, should not warn")
}

func TestValidatePriorityOverrides_EnabledScraper(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"r18dev": scraperSettings(true),
	})
	cfg.Metadata.Priority.Fields["title"] = []string{"r18dev"}

	warnings := ValidatePriorityOverrides(cfg)
	assert.Empty(t, warnings, "enabled scraper should not generate a warning")
}

func TestValidatePriorityOverrides_UnknownScraper(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm": scraperSettings(false),
	})
	cfg.Metadata.Priority.Fields["content_id"] = []string{"unknownscraper"}

	warnings := ValidatePriorityOverrides(cfg)
	assert.Empty(t, warnings, "unknown scrapers should be skipped, no warning")
}

func TestValidatePriorityOverrides_MixedKnownDisabledAndUnknown(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm": scraperSettings(false),
	})
	cfg.Metadata.Priority.Fields["content_id"] = []string{"dmm", "unknownscraper"}

	warnings := ValidatePriorityOverrides(cfg)
	require.Len(t, warnings, 1)
	assert.Equal(t, []string{"dmm"}, warnings[0].Scrapers, "warning should list only known disabled scrapers")
}

func TestValidatePriorityOverrides_MultipleFields(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm": scraperSettings(false),
	})
	cfg.Metadata.Priority.Fields["content_id"] = []string{"dmm"}
	cfg.Metadata.Priority.Fields["title"] = []string{"dmm"}
	cfg.Metadata.Priority.Fields["id"] = []string{"dmm"}

	warnings := ValidatePriorityOverrides(cfg)
	require.Len(t, warnings, 3, "should have one warning per field")
	assert.Equal(t, "content_id", warnings[0].Field, "should be sorted by field name")
	assert.Equal(t, "id", warnings[1].Field)
	assert.Equal(t, "title", warnings[2].Field)
}

func TestValidatePriorityOverrides_NilConfig(t *testing.T) {
	warnings := ValidatePriorityOverrides(nil)
	assert.Nil(t, warnings)
}

func TestValidatePriorityOverrides_NilFields(t *testing.T) {
	cfg := &Config{
		Scrapers: ScrapersConfig{},
		Metadata: MetadataConfig{},
	}
	warnings := ValidatePriorityOverrides(cfg)
	assert.Nil(t, warnings)
}

func TestValidatePriorityOverrides_WarningsSurviveClone(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm": scraperSettings(false),
	})
	cfg.Metadata.Priority.Fields["content_id"] = []string{"dmm"}
	cfg.Warnings = ValidatePriorityOverrides(cfg)
	require.Len(t, cfg.Warnings, 1)

	cloned := cfg.Clone()
	require.Len(t, cloned.Warnings, 1)
	assert.Equal(t, cfg.Warnings[0].Field, cloned.Warnings[0].Field)
	assert.Equal(t, cfg.Warnings[0].Scrapers, cloned.Warnings[0].Scrapers)

	// Verify deep-copy: mutating clone's scrapers slice doesn't affect original
	if len(cloned.Warnings) > 0 {
		cloned.Warnings[0].Scrapers[0] = "mutated"
		assert.NotEqual(t, "mutated", cfg.Warnings[0].Scrapers[0], "clone should be a deep copy")
	}
}

func TestValidatePriorityOverrides_SurvivesRedact(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm": scraperSettings(false),
	})
	cfg.Metadata.Priority.Fields["content_id"] = []string{"dmm"}
	cfg.Warnings = ValidatePriorityOverrides(cfg)
	require.Len(t, cfg.Warnings, 1)

	redacted := cfg.Redact()
	require.Len(t, redacted.Warnings, 1, "warnings should survive Redact (which calls Clone)")
	assert.Equal(t, "content_id", redacted.Warnings[0].Field)
}

func TestValidate_SetsWarningsOnOriginal(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm": scraperSettings(false),
	})
	cfg.Metadata.Priority.Fields["content_id"] = []string{"dmm"}
	cfg.Database.Type = "sqlite"
	cfg.Database.DSN = "/tmp/test.db"
	cfg.Scrapers.TimeoutSeconds = 30
	cfg.Scrapers.RequestTimeoutSeconds = 60
	cfg.Scrapers.Priority = []string{"r18dev"}
	cfg.Scrapers.Overrides["r18dev"] = scraperSettings(true)
	cfg.Performance.MaxWorkers = 5
	cfg.Performance.WorkerTimeout = 300
	cfg.Performance.UpdateInterval = 100

	err := cfg.Validate()
	require.NoError(t, err)
	require.Len(t, cfg.Warnings, 1, "Validate should set Warnings on the original config")
	assert.Equal(t, "content_id", cfg.Warnings[0].Field)
}

func TestValidatePriorityOverrides_EnabledButNotInPriority(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm":    scraperSettings(true),
		"r18dev": scraperSettings(true),
	})
	cfg.Scrapers.Priority = []string{"r18dev"} // DMM not in priority
	cfg.Metadata.Priority.Fields["content_id"] = []string{"dmm"}

	warnings := ValidatePriorityOverrides(cfg)
	require.Len(t, warnings, 1, "enabled scraper not in priority list should warn")
	assert.Equal(t, "content_id", warnings[0].Field)
	assert.Contains(t, warnings[0].Scrapers, "dmm")
}

func TestValidatePriorityOverrides_EnabledAndInPriority(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm":    scraperSettings(true),
		"r18dev": scraperSettings(true),
	})
	cfg.Scrapers.Priority = []string{"dmm", "r18dev"}
	cfg.Metadata.Priority.Fields["content_id"] = []string{"dmm"}

	warnings := ValidatePriorityOverrides(cfg)
	assert.Empty(t, warnings, "enabled scraper in priority list should not warn")
}

func TestValidatePriorityOverrides_EmptyPriorityList(t *testing.T) {
	cfg := newConfigWithOverrides(map[string]*scraperconfig.ScraperSettings{
		"dmm": scraperSettings(true),
	})
	cfg.Scrapers.Priority = []string{} // empty = all scrapers in priority
	cfg.Metadata.Priority.Fields["content_id"] = []string{"dmm"}

	warnings := ValidatePriorityOverrides(cfg)
	assert.Empty(t, warnings, "empty priority list means all scrapers are queryable")
}
