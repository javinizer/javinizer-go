package scrape

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helperToTranslator constructs a Translator from *config.TranslationConfig
// using the same bridge logic as NewTranslatorFromApp, but available for tests.
func helperToTranslator(cfg *config.TranslationConfig) Translator {
	return NewTranslatorFromApp(cfg)
}

func TestApplyTranslation_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": `["タイトル翻訳","説明翻訳"]`,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer ts.Close()

	translationCfg := &config.TranslationConfig{
		Enabled:                 true,
		Provider:                "openai",
		SourceLanguage:          "en",
		TargetLanguage:          "ja",
		ApplyToPrimary:          true,
		OverwriteExistingTarget: true,
		OpenAI: config.OpenAITranslationConfig{
			BaseURL: ts.URL,
			APIKey:  "k",
			Model:   "m",
		},
		Fields: config.TranslationFieldsConfig{
			Title:       true,
			Description: true,
		},
	}

	movie := &models.Movie{
		ID:          "IPX-001",
		ContentID:   "ipx001",
		Title:       "Original Title",
		Description: "Original Description",
	}

	translator := helperToTranslator(translationCfg)
	warning, _, _ := applyTranslation(context.Background(), movie, translator)
	assert.Empty(t, warning)
	assert.Len(t, movie.Translations, 1)

	jaTrans := movie.Translations[0]
	assert.Equal(t, "ja", jaTrans.Language)
	assert.Equal(t, "タイトル翻訳", jaTrans.Title)
	assert.Equal(t, "translation:openai", jaTrans.SourceName)
}

func TestApplyTranslation_FailureReturnsWarning(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	translationCfg := &config.TranslationConfig{
		Enabled:        true,
		Provider:       "openai",
		SourceLanguage: "en",
		TargetLanguage: "ja",
		OpenAI: config.OpenAITranslationConfig{
			BaseURL: ts.URL,
			APIKey:  "k",
			Model:   "m",
		},
		Fields: config.TranslationFieldsConfig{Title: true},
	}

	movie := &models.Movie{
		ID:        "IPX-002",
		ContentID: "ipx002",
		Title:     "Original Title",
	}

	translator := helperToTranslator(translationCfg)
	warning, _, _ := applyTranslation(context.Background(), movie, translator)
	assert.NotEmpty(t, warning)
	assert.Equal(t, "Original Title", movie.Title)
}

// TestApplyTranslation_DegradedEmitsSingleStructuredWarn proves the degraded
// path (partial per-field failure with no provider error) surfaces the
// degraded code and emits exactly one structured Warn WITHOUT a status_code
// field, via the production translateWithContext emission point.
func TestApplyTranslation_DegradedEmitsSingleStructuredWarn(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": `["   "]`,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer ts.Close()

	logPath := filepath.Join(t.TempDir(), "apply-translation-test.log")
	require.NoError(t, logging.InitLogger(&logging.Config{Level: "debug", Format: "json", Output: logPath}))
	t.Cleanup(func() { logging.CloseLogger() })

	translationCfg := &config.TranslationConfig{
		Enabled:        true,
		Provider:       "openai",
		SourceLanguage: "ja",
		TargetLanguage: "en",
		ApplyToPrimary: true,
		OpenAI: config.OpenAITranslationConfig{
			BaseURL: ts.URL,
			APIKey:  "k",
			Model:   "m",
		},
		Fields: config.TranslationFieldsConfig{Title: true},
	}

	movie := &models.Movie{
		ID:    "DEG-001",
		Title: "タイトル",
	}

	translator := helperToTranslator(translationCfg)
	warning, code, _ := applyTranslation(context.Background(), movie, translator)
	assert.Equal(t, "degraded", code)
	assert.Contains(t, warning, "empty translation")

	entries := warningLogEntries(t, logPath, "DEG-001")
	require.Len(t, entries, 1, "degraded movie emits exactly one structured Warn")
	entry := entries[0]
	assert.Equal(t, "degraded", entry["warning_code"])
	assert.Equal(t, "openai", entry["provider"])
	assert.Equal(t, "ja", entry["source_lang"])
	assert.Equal(t, "en", entry["target_lang"])
	_, hasStatus := entry["status_code"]
	assert.False(t, hasStatus, "degraded classification attaches no status_code")
}

// TestTranslateWithContext_NilOutputBranches covers translateWithContext's
// early "no translation record" return: the service reports nil output
// (disabled) or an output with a nil Movie (no translatable fields), so the
// wrapper returns empty warning/code with a nil output.
func TestTranslateWithContext_NilOutputBranches(t *testing.T) {
	movie := &models.Movie{ID: "NIL-OUT-1", Title: "タイトル"}

	tests := []struct {
		name       string
		translator Translator
	}{
		{
			name: "disabled service returns nil output",
			translator: &translationAdapter{
				svc: newTranslationService("openai", "ja", "en", "hash", 0, false,
					translation.New(translation.Config{Enabled: false, Provider: "openai", SourceLanguage: "ja", TargetLanguage: "en"})),
				enabled:  true,
				provider: "openai",
			},
		},
		{
			name: "no translatable fields returns output with nil movie",
			translator: helperToTranslator(&config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				SourceLanguage: "ja",
				TargetLanguage: "en",
				OpenAI:         config.OpenAITranslationConfig{BaseURL: "http://127.0.0.1:1", APIKey: "k", Model: "m"},
				Fields:         config.TranslationFieldsConfig{},
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warning, code, output := applyTranslation(context.Background(), movie, tt.translator)
			assert.Empty(t, warning)
			assert.Empty(t, code)
			assert.Nil(t, output)
		})
	}
}

// TestLogTranslationWarning covers logTranslationWarning's legacy unstructured
// branch (unclassified error with an empty code), its ContentID fallback for
// the log identity, and the context-cancellation suppression: a Warn is
// emitted only while the request context is live.
func TestLogTranslationWarning(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log-translation-warning.log")
	require.NoError(t, logging.InitLogger(&logging.Config{Level: "debug", Format: "json", Output: logPath}))
	t.Cleanup(func() { logging.CloseLogger() })

	ts := newTranslationService("openai", "ja", "en", "hash", 60, false,
		translation.New(translation.Config{Provider: "openai"}))
	configErr := errors.New("target language is required")

	liveCtx := context.Background()
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name        string
		ctx         context.Context
		movie       *models.Movie
		code        translation.TranslationWarningCode
		err         error
		wantEntries []string // substrings that MUST appear in warning entries
		wantAbsent  []string // substrings that MUST NOT appear in any log line
	}{
		{
			name:  "structured warn falls back to ContentID when ID is empty",
			ctx:   liveCtx,
			movie: &models.Movie{ID: "", ContentID: "CID-RL-1", Title: "t"},
			code:  translation.TranslationWarningRateLimited,
			wantEntries: []string{
				`"movie_id":"CID-RL-1"`,
				`"warning_code":"rate_limited"`,
			},
		},
		{
			name:        "legacy unstructured warn for unclassified config error with live context",
			ctx:         liveCtx,
			movie:       &models.Movie{ID: "LEG-1", Title: "t"},
			code:        "",
			err:         configErr,
			wantEntries: []string{"[LEG-1] Metadata translation failed: target language is required"},
		},
		{
			name:        "legacy warn uses ContentID when ID is empty",
			ctx:         liveCtx,
			movie:       &models.Movie{ID: "", ContentID: "CID-LEG-9", Title: "t"},
			code:        "",
			err:         configErr,
			wantEntries: []string{"[CID-LEG-9] Metadata translation failed: target language is required"},
		},
		{
			name:       "canceled context suppresses the legacy warn",
			ctx:        canceledCtx,
			movie:      &models.Movie{ID: "LEG-SUPPRESSED-1", Title: "t"},
			code:       "",
			err:        configErr,
			wantAbsent: []string{"LEG-SUPPRESSED-1"},
		},
		{
			name:       "empty code with nil error logs nothing",
			ctx:        liveCtx,
			movie:      &models.Movie{ID: "LEG-NOOP-1", Title: "t"},
			code:       "",
			err:        nil,
			wantAbsent: []string{"LEG-NOOP-1"},
		},
	}

	var wantCount int
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.logTranslationWarning(tt.ctx, tt.movie, tt.code, tt.err)
		})
		if tt.code != "" || (tt.err != nil && tt.ctx.Err() == nil) {
			wantCount++
		}
	}

	raw, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logs := string(raw)
	for _, tt := range tests {
		for _, want := range tt.wantEntries {
			assert.Contains(t, logs, want, tt.name)
		}
		for _, absent := range tt.wantAbsent {
			assert.NotContains(t, logs, absent, tt.name)
		}
	}

	warningLines := 0
	for _, line := range strings.Split(logs, "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry["level"] == "warning" {
			warningLines++
		}
	}
	assert.Equal(t, wantCount, warningLines, "exactly the live-ctx error cases emit a Warn")
}

// TestPostProcessScraped_TranslationEnabledStampsWarning covers the live-scrape
// assembly: with translation enabled the ScrapeResult carries applyTranslation's
// warning string and machine-readable code.
func TestPostProcessScraped_TranslationEnabledStampsWarning(t *testing.T) {
	stub := &stubWarningTranslatorCacheTest{warning: "Translation (openai): rate limited", code: "rate_limited"}
	cfg := &Config{TranslationEnabled: true}
	movie := &models.Movie{ID: "PP-1", Title: "t"}

	result, err := postProcessScraped(context.Background(), movie, nil, nil, cfg, stub, nil, ScrapeCmd{MovieID: "PP-1"}, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Translation (openai): rate limited", result.TranslationWarning)
	assert.Equal(t, "rate_limited", result.TranslationWarningCode)
}

func TestApplyTranslation_NilMovie(t *testing.T) {
	translator := &translationAdapter{svc: nil, enabled: true, provider: "test"}
	warning, _, _ := applyTranslation(context.Background(), nil, translator)
	assert.Empty(t, warning)
}

func TestApplyTranslation_NilTranslator(t *testing.T) {
	movie := &models.Movie{Title: "test"}
	warning, _, _ := applyTranslation(context.Background(), movie, nil)
	assert.Empty(t, warning)
}

func TestApplyTranslation_NoOpTranslator(t *testing.T) {
	movie := &models.Movie{Title: "test"}
	translator := noOpTranslator{}
	warning, _, _ := applyTranslation(context.Background(), movie, translator)
	assert.Empty(t, warning)
}

func TestApplyTranslation_WarningOnProviderError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer ts.Close()

	translationCfg := &config.TranslationConfig{
		Enabled:        true,
		Provider:       "openai",
		SourceLanguage: "en",
		TargetLanguage: "ja",
		OpenAI: config.OpenAITranslationConfig{
			BaseURL: ts.URL,
			APIKey:  "k",
			Model:   "m",
		},
		Fields: config.TranslationFieldsConfig{Title: true},
	}

	movie := &models.Movie{
		ID:        "IPX-003",
		ContentID: "ipx003",
		Title:     "Original Title",
	}

	translator := helperToTranslator(translationCfg)
	warning, _, _ := applyTranslation(context.Background(), movie, translator)
	assert.Contains(t, warning, "rate limited")
	assert.Equal(t, "Original Title", movie.Title)
}

func TestApplyTranslation_WarningOnEmptyResult(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": `[""]`}},
			},
		})
	}))
	defer ts.Close()

	translationCfg := &config.TranslationConfig{
		Enabled:        true,
		Provider:       "openai",
		SourceLanguage: "en",
		TargetLanguage: "ja",
		ApplyToPrimary: true,
		OpenAI: config.OpenAITranslationConfig{
			BaseURL: ts.URL,
			APIKey:  "k",
			Model:   "m",
		},
		Fields: config.TranslationFieldsConfig{Title: true},
	}

	movie := &models.Movie{
		ID:        "IPX-004",
		ContentID: "ipx004",
		Title:     "Original Title",
	}

	translator := helperToTranslator(translationCfg)
	warning, _, _ := applyTranslation(context.Background(), movie, translator)
	assert.Contains(t, warning, "title: empty translation, kept original")
	assert.Equal(t, "Original Title", movie.Title)
}

func TestMergeOrAppendTranslation(t *testing.T) {
	tests := []struct {
		name      string
		existing  []models.MovieTranslation
		incoming  models.MovieTranslation
		overwrite bool
		wantLen   int
		wantJA    *models.MovieTranslation
	}{
		{
			name:      "empty language returns existing unchanged",
			existing:  []models.MovieTranslation{{Language: "en", Title: "English Title"}},
			incoming:  models.MovieTranslation{Language: "  ", Title: "Ignored"},
			overwrite: false,
			wantLen:   1,
			wantJA:    nil,
		},
		{
			name:      "new language appends to existing",
			existing:  []models.MovieTranslation{{Language: "en", Title: "English Title"}},
			incoming:  models.MovieTranslation{Language: "ja", Title: "Japanese Title"},
			overwrite: false,
			wantLen:   2,
			wantJA:    &models.MovieTranslation{Language: "ja", Title: "Japanese Title"},
		},
		{
			name:      "existing language with overwrite true merges fields",
			existing:  []models.MovieTranslation{{Language: "en", Title: "Old English"}},
			incoming:  models.MovieTranslation{Language: "en", Title: "New English", Description: "New Description"},
			overwrite: true,
			wantLen:   1,
			wantJA:    nil,
		},
		{
			name:      "existing language with overwrite false keeps existing",
			existing:  []models.MovieTranslation{{Language: "en", Title: "Old English"}},
			incoming:  models.MovieTranslation{Language: "en", Title: "New English"},
			overwrite: false,
			wantLen:   1,
			wantJA:    nil,
		},
		{
			name:      "language matching is case-insensitive",
			existing:  []models.MovieTranslation{{Language: "en", Title: "English Title"}},
			incoming:  models.MovieTranslation{Language: "EN", Title: "Uppercase EN"},
			overwrite: false,
			wantLen:   1,
			wantJA:    nil,
		},
		{
			name:      "trim whitespace before comparison",
			existing:  []models.MovieTranslation{{Language: "en", Title: "English Title"}},
			incoming:  models.MovieTranslation{Language: " ja ", Title: "Japanese"},
			overwrite: false,
			wantLen:   2,
			wantJA:    &models.MovieTranslation{Language: " ja ", Title: "Japanese"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeOrAppendTranslation(tt.existing, tt.incoming, tt.overwrite)

			assert.Len(t, got, tt.wantLen, "unexpected number of translations")

			if tt.wantJA != nil {
				found := false
				for _, tr := range got {
					if tr.Language == tt.wantJA.Language && tr.Title == tt.wantJA.Title {
						found = true
						break
					}
				}
				assert.True(t, found, "expected to find incoming translation")
			}
		})
	}
}

func TestMergeTranslationFields(t *testing.T) {
	t.Run("overwrites all non-empty incoming fields", func(t *testing.T) {
		current := models.MovieTranslation{
			Language:      "en",
			Title:         "Old Title",
			OriginalTitle: "Old Original",
			Description:   "Old Description",
			Director:      "Old Director",
			Maker:         "Old Maker",
			Label:         "Old Label",
			Series:        "Old Series",
			SourceName:    "old-source",
		}
		incoming := models.MovieTranslation{
			Language:      "ja",
			Title:         "New Title",
			OriginalTitle: "New Original",
			Description:   "New Description",
			Director:      "New Director",
			Maker:         "New Maker",
			Label:         "New Label",
			Series:        "New Series",
			SourceName:    "new-source",
		}

		merged := mergeTranslationFields(current, incoming)
		assert.Equal(t, "ja", merged.Language)
		assert.Equal(t, "New Title", merged.Title)
		assert.Equal(t, "New Original", merged.OriginalTitle)
		assert.Equal(t, "New Description", merged.Description)
		assert.Equal(t, "New Director", merged.Director)
		assert.Equal(t, "New Maker", merged.Maker)
		assert.Equal(t, "New Label", merged.Label)
		assert.Equal(t, "New Series", merged.Series)
		assert.Equal(t, "new-source", merged.SourceName)
	})

	t.Run("keeps existing values when incoming fields are empty", func(t *testing.T) {
		current := models.MovieTranslation{
			Language:      "en",
			Title:         "Old Title",
			OriginalTitle: "Old Original",
			Description:   "Old Description",
			Director:      "Old Director",
			Maker:         "Old Maker",
			Label:         "Old Label",
			Series:        "Old Series",
			SourceName:    "old-source",
		}
		incoming := models.MovieTranslation{
			Language: "fr",
		}

		merged := mergeTranslationFields(current, incoming)
		assert.Equal(t, "fr", merged.Language)
		assert.Equal(t, "Old Title", merged.Title)
		assert.Equal(t, "Old Original", merged.OriginalTitle)
		assert.Equal(t, "Old Description", merged.Description)
		assert.Equal(t, "Old Director", merged.Director)
		assert.Equal(t, "Old Maker", merged.Maker)
		assert.Equal(t, "Old Label", merged.Label)
		assert.Equal(t, "Old Series", merged.Series)
		assert.Equal(t, "old-source", merged.SourceName)
	})
}
