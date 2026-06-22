package scrape

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubWarningTranslatorCacheTest returns a fixed warning + translated=true
// so applyTranslation has a non-empty warning to propagate. It does NOT mutate
// the movie — that keeps the test focused on cache.go's field plumbing rather
// than the translation logic itself (already covered by apply_translation tests).
type stubWarningTranslatorCacheTest struct {
	warning string
}

func (s *stubWarningTranslatorCacheTest) Translate(_ context.Context, _ *models.Movie) (string, bool, *translation.TranslationOutput) {
	return s.warning, true, nil
}

// TestTryCache_RetranslationSettingsChangePropagatesTranslationWarning is a
// regression test for the "field dropped on rebuild/fallback path" pattern.
//
// Bug (commit fixing this): internal/scrape/cache.go's tryCache computed
// `warn := applyTranslation(ctx, cached, s.translator)` on the
// "translation settings changed → re-translate the cached movie" branch,
// logged it, then DISCARDED it — the returned *ScrapeResult had an empty
// TranslationWarning even though applyTranslation had produced a non-empty
// warning. The happy-path postProcessScraped (scrape.go:242) DID set
// TranslationWarning on its ScrapeResult, but cache.go did not. Same pattern
// as commits 83fba0c5 / d9106a96 / 42d89e65 / 6249de64 / 6ed5d0e5 — a fresh
// struct (ScrapeResult) built on a "fallback / cache-hit / alternate" path
// dropped a field main's happy path populated.
//
// Downstream consumer: workflow/scrape_orchestrator.go:91-93 reads
// result.TranslationWarning and copies it into meta.TranslationWarning;
// worker/movie_result.go stamps mr.OrchestrationState = meta.OrchestrationState;
// api/batch/convert.go surfaces it as `translation_warning` on the API response.
// So a cache-hit with re-translated settings previously surfaced an empty
// warning even when the re-translation was partial.
func TestTryCache_RetranslationSettingsChangePropagatesTranslationWarning(t *testing.T) {
	// Build the scraper with translation ENABLED, but with settings that
	// differ from what the cached movie was translated under → forces the
	// re-translation branch. Use an in-memory SQLite DB (:memory:) so the
	// test has no filesystem dependency.
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.TargetLanguage = "en"
	cfg.Metadata.Translation.Provider = "deepl"
	cfg.Metadata.Translation.DeepL.APIKey = "dummy-test-key"
	// SettingsHash is derived from the translation config; we set the field
	// explicitly below so the cache-hit movie's stored translation has a
	// DIFFERENT (stale) hash → forces re-translation.
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	movieRepo := database.NewMovieRepository(db)

	// Seed a cached movie with a STALE translation (SettingsHash mismatch →
	// the !hasValidTranslation branch fires → applyTranslation runs → the
	// translator returns a warning that the test expects to see on
	// result.TranslationWarning).
	cachedMovie := &models.Movie{
		ID:    "ABC-001",
		Title: "Cached Title",
		Translations: []models.MovieTranslation{
			{
				Language:     "en",
				Title:        "Cached English Title",
				SettingsHash: "stale-hash-0001", // different from current config's hash
			},
		},
	}
	_, err = movieRepo.Upsert(context.Background(), cachedMovie)
	require.NoError(t, err)

	expectedWarning := "partial translation: timeout from OpenAI"
	translator := &stubWarningTranslatorCacheTest{warning: expectedWarning}

	scrapeCfg := &Config{
		ScrapersPriority:      cfg.Scrapers.Priority,
		TranslationEnabled:    true,
		TranslationTargetLang: "en",
		TranslationSettingsHash: func() string {
			// any non-"stale-hash-0001" value triggers re-translation
			return "current-hash-9999"
		}(),
	}
	s := New(nil, nil, database.NewActressRepository(db), movieRepo, nil, scrapeCfg, translator, nil)

	result := s.tryCache(context.Background(), ScrapeCmd{MovieID: "ABC-001"}, nil, time.Now())

	require.NotNil(t, result, "cache hit should return a non-nil ScrapeResult")
	require.False(t, result.NeedsPersistence == false && result.TranslationWarning == "",
		"re-translation branch should have run and set either NeedsPersistence or TranslationWarning")
	assert.True(t, result.NeedsPersistence, "re-translated cache hit must set NeedsPersistence for re-persistence")
	assert.Equal(t, expectedWarning, result.TranslationWarning,
		"cache-hit re-translation must surface applyTranslation's warning on ScrapeResult.TranslationWarning — "+
			"the field was computed and logged but discarded before this fix (same dropped-on-fallback-path "+
			"pattern as 83fba0c5 / 42d89e65 / 6ed5d0e5)")
}
