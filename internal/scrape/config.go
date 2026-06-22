package scrape

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
)

// Config holds the subset of application configuration needed by the Scrape seam.
type Config struct {
	ScrapersPriority        []string
	TranslationEnabled      bool
	TranslationSettingsHash string
	TranslationTargetLang   string
	ActressDBEnabled        bool
	ScrapeActress           bool
	UserAgent               string
	Referer                 string
	TempDir                 string
}

// Translator is the interface for applying metadata translation to a scraped Movie.
// Replaces the former TranslationConfigView closure — callers provide an implementation
// rather than constructing a closure. The production adapter wraps the translation service;
// test adapters return canned results.
type Translator interface {
	// Translate applies metadata translation to the given movie in-place.
	// Returns a warning string if translation partially failed, or empty string on success.
	// The translated bool indicates whether any translation was attempted.
	// The *translation.TranslationOutput carries genre/actress translation data for
	// persistence; nil when translation was not performed or produced no output.
	Translate(ctx context.Context, movie *models.Movie) (warning string, translated bool, output *translation.TranslationOutput)
}

// noOpTranslator is returned when translation is disabled. It satisfies the Translator
// interface without doing any work, so the Scraper never needs a nil check.
type noOpTranslator struct{}

func (noOpTranslator) Translate(_ context.Context, _ *models.Movie) (string, bool, *translation.TranslationOutput) {
	return "", false, nil
}

// NewTranslatorFromApp constructs a Translator from *config.TranslationConfig.
// This is the bridge — the only place the scrape package imports config for translation.
// Returns a noOpTranslator when translation is disabled.
//
// Config-bridge reads: cfg.Metadata.Translation.Enabled, cfg.Metadata.Translation.Provider,
// cfg.Metadata.Translation.SourceLanguage, cfg.Metadata.Translation.TargetLanguage,
// cfg.Metadata.Translation.SettingsHash(), cfg.Metadata.Translation.TimeoutSeconds,
// cfg.Metadata.Translation.OverwriteExistingTarget
func NewTranslatorFromApp(cfg *config.TranslationConfig) Translator {
	if cfg == nil || !cfg.Enabled {
		return noOpTranslator{}
	}
	bridgeCfg := translation.ConfigFromApp(*cfg)
	httpClient := newTranslationHTTPClient()
	ts := translation.New(bridgeCfg,
		translation.NewOpenAIProvider(bridgeCfg, httpClient),
		translation.NewOpenAICompatibleProvider(bridgeCfg, httpClient),
		translation.NewDeepLProvider(bridgeCfg, httpClient),
		translation.NewGoogleProvider(bridgeCfg, httpClient),
		translation.NewAnthropicProvider(bridgeCfg, httpClient),
	)
	svc := newTranslationService(
		cfg.Provider,
		cfg.SourceLanguage,
		cfg.TargetLanguage,
		cfg.SettingsHash(),
		cfg.TimeoutSeconds,
		cfg.OverwriteExistingTarget,
		ts,
	)
	return &translationAdapter{
		svc:      svc,
		enabled:  true,
		provider: cfg.Provider,
	}
}

// translationAdapter wraps a translationService to satisfy the Translator interface.
// This is the production adapter — the only one that performs real translation.
type translationAdapter struct {
	svc      *translationService
	enabled  bool
	provider string
}

func (a *translationAdapter) Translate(ctx context.Context, movie *models.Movie) (string, bool, *translation.TranslationOutput) {
	if movie == nil {
		return "", false, nil
	}
	warning, output := a.svc.translateWithContext(ctx, movie)
	return warning, true, output
}

// ConfigFromAppConfig extracts Scrape-relevant fields from the application config.
//
// Config-bridge reads: cfg.Scrapers.Priority, cfg.Metadata.Translation.Enabled,
// cfg.Metadata.Translation.TargetLanguage, cfg.Metadata.Translation.SettingsHash(),
// cfg.Metadata.ActressDatabase.Enabled, cfg.Scrapers.ScrapeActress,
// cfg.Scrapers.UserAgent, cfg.Scrapers.Referer, cfg.System.TempDir
func ConfigFromAppConfig(cfg *config.Config) *Config {
	if cfg == nil {
		return nil
	}
	c := &Config{
		ScrapersPriority:      cfg.Scrapers.Priority,
		TranslationEnabled:    cfg.Metadata.Translation.Enabled,
		TranslationTargetLang: cfg.Metadata.Translation.TargetLanguage,
		ActressDBEnabled:      cfg.Metadata.ActressDatabase.Enabled,
		ScrapeActress:         cfg.Scrapers.ScrapeActress,
		UserAgent:             cfg.Scrapers.UserAgent,
		Referer:               cfg.Scrapers.Referer,
		TempDir:               cfg.System.TempDir,
	}
	if c.TranslationEnabled {
		c.TranslationSettingsHash = cfg.Metadata.Translation.SettingsHash()
	}
	return c
}
