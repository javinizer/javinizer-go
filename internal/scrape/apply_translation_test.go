package scrape

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
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
	warning, _ := applyTranslation(context.Background(), movie, translator)
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
	warning, _ := applyTranslation(context.Background(), movie, translator)
	assert.NotEmpty(t, warning)
	assert.Equal(t, "Original Title", movie.Title)
}

func TestApplyTranslation_NilMovie(t *testing.T) {
	translator := &translationAdapter{svc: nil, enabled: true, provider: "test"}
	warning, _ := applyTranslation(context.Background(), nil, translator)
	assert.Empty(t, warning)
}

func TestApplyTranslation_NilTranslator(t *testing.T) {
	movie := &models.Movie{Title: "test"}
	warning, _ := applyTranslation(context.Background(), movie, nil)
	assert.Empty(t, warning)
}

func TestApplyTranslation_NoOpTranslator(t *testing.T) {
	movie := &models.Movie{Title: "test"}
	translator := noOpTranslator{}
	warning, _ := applyTranslation(context.Background(), movie, translator)
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
	warning, _ := applyTranslation(context.Background(), movie, translator)
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
	warning, _ := applyTranslation(context.Background(), movie, translator)
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
