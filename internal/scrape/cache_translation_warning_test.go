package scrape

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
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
	code    string
}

func (s *stubWarningTranslatorCacheTest) Translate(_ context.Context, _ *models.Movie) (string, string, bool, *translation.TranslationOutput) {
	return s.warning, s.code, true, nil
}

// TestTryCache_RetranslationSettingsChangePropagatesTranslationWarning proves
// the stale-hash cache-hit re-translation branch surfaces the warning (and code)
// on the rebuilt ScrapeResult instead of discarding it.
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
	translator := &stubWarningTranslatorCacheTest{warning: expectedWarning, code: "degraded"}

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
		"cache-hit re-translation must surface applyTranslation's warning on ScrapeResult.TranslationWarning")
	assert.Equal(t, "degraded", result.TranslationWarningCode,
		"cache-hit re-translation must surface the machine-readable warning code alongside the string")
}

// warningLogEntries returns the parsed warning-level log entries that carry
// the given movie_id, from a JSON-format log file produced by initLoggerToFile.
func warningLogEntries(t *testing.T, logPath, movieID string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(logPath)
	require.NoError(t, err)
	var entries []map[string]any
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry["level"] == "warning" && entry["movie_id"] == movieID {
			entries = append(entries, entry)
		}
	}
	return entries
}

// TestTryCache_RetranslationEmitsSingleStructuredWarn proves the cache-hit
// re-translation path emits exactly ONE structured Warn per movie (with the
// full field set) while surfacing BOTH translation_warning and
// translation_warning_code on the rebuilt ScrapeResult — regression coverage
// for the previous two-unstructured-Warn behavior (sanitize-path + cache-path).
func TestTryCache_RetranslationEmitsSingleStructuredWarn(t *testing.T) {
	rateLimitedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer rateLimitedSrv.Close()

	logPath := filepath.Join(t.TempDir(), "scrape-test.log")
	require.NoError(t, logging.InitLogger(&logging.Config{Level: "debug", Format: "json", Output: logPath}))
	t.Cleanup(func() { logging.CloseLogger() })

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "openai"
	cfg.Metadata.Translation.SourceLanguage = "ja"
	cfg.Metadata.Translation.TargetLanguage = "en"
	cfg.Metadata.Translation.Fields.Title = true
	cfg.Metadata.Translation.OpenAI.BaseURL = rateLimitedSrv.URL
	cfg.Metadata.Translation.OpenAI.APIKey = "test-key"
	cfg.Metadata.Translation.OpenAI.Model = "gpt-4o-mini"
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })

	movieRepo := database.NewMovieRepository(db)
	cachedMovie := &models.Movie{
		ID:    "ABC-429",
		Title: "キャッシュされたタイトル",
		Translations: []models.MovieTranslation{
			{Language: "en", Title: "Cached English Title", SettingsHash: "stale-hash-0001"},
		},
	}
	_, err = movieRepo.Upsert(context.Background(), cachedMovie)
	require.NoError(t, err)

	translator := NewTranslatorFromApp(&cfg.Metadata.Translation)
	scrapeCfg := &Config{
		ScrapersPriority:        cfg.Scrapers.Priority,
		TranslationEnabled:      true,
		TranslationTargetLang:   "en",
		TranslationSettingsHash: "current-hash-9999",
	}
	s := New(nil, nil, database.NewActressRepository(db), movieRepo, nil, scrapeCfg, translator, nil)

	result := s.tryCache(context.Background(), ScrapeCmd{MovieID: "ABC-429"}, nil, time.Now())

	require.NotNil(t, result, "cache hit should return a non-nil ScrapeResult")
	assert.Equal(t, "rate_limited", result.TranslationWarningCode)
	assert.Contains(t, result.TranslationWarning, "rate limited")

	entries := warningLogEntries(t, logPath, "ABC-429")
	require.Len(t, entries, 1, "exactly one structured Warn per movie on the cache-hit path")
	entry := entries[0]
	assert.Equal(t, "openai", entry["provider"])
	assert.Equal(t, "", entry["mode"], "openai has no provider mode")
	assert.Equal(t, "ja", entry["source_lang"])
	assert.Equal(t, "en", entry["target_lang"])
	assert.Equal(t, "rate_limited", entry["warning_code"])
	assert.EqualValues(t, 429, entry["status_code"], "HTTP-status classification carries status_code")
}
