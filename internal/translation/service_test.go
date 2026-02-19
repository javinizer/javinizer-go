package translation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranslateMovieOpenAI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		response := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": `["Translated Title","Translated Description"]`,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer ts.Close()

	svc := New(config.TranslationConfig{
		Enabled:        true,
		Provider:       "openai",
		SourceLanguage: "en",
		TargetLanguage: "ja",
		ApplyToPrimary: true,
		Fields: config.TranslationFieldsConfig{
			Title:       true,
			Description: true,
		},
		OpenAI: config.OpenAITranslationConfig{
			BaseURL: ts.URL,
			APIKey:  "test-key",
			Model:   "test-model",
		},
	})

	movie := &models.Movie{
		Title:       "Original Title",
		Description: "Original Description",
	}

	translated, err := svc.TranslateMovie(context.Background(), movie)
	require.NoError(t, err)
	require.NotNil(t, translated)

	assert.Equal(t, "Translated Title", movie.Title)
	assert.Equal(t, "Translated Description", movie.Description)
	assert.Equal(t, "ja", translated.Language)
	assert.Equal(t, "Translated Title", translated.Title)
	assert.Equal(t, "Translated Description", translated.Description)
}

func TestTranslateMovieDeepLNoPrimaryOverride(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/translate", r.URL.Path)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "DEEPL_KEY", r.FormValue("auth_key"))
		assert.Equal(t, "JA", r.FormValue("target_lang"))
		assert.Equal(t, "EN", r.FormValue("source_lang"))

		response := map[string]any{
			"translations": []map[string]any{
				{"text": "DeepL Title"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer ts.Close()

	svc := New(config.TranslationConfig{
		Enabled:        true,
		Provider:       "deepl",
		SourceLanguage: "en",
		TargetLanguage: "ja",
		ApplyToPrimary: false,
		Fields: config.TranslationFieldsConfig{
			Title: true,
		},
		DeepL: config.DeepLTranslationConfig{
			Mode:    "free",
			BaseURL: ts.URL,
			APIKey:  "DEEPL_KEY",
		},
	})

	movie := &models.Movie{Title: "Original Title"}
	translated, err := svc.TranslateMovie(context.Background(), movie)
	require.NoError(t, err)
	require.NotNil(t, translated)

	assert.Equal(t, "Original Title", movie.Title, "primary field should be untouched when apply_to_primary=false")
	assert.Equal(t, "DeepL Title", translated.Title)
}

func TestTranslateMovieGoogleFree(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/translate_a/single", r.URL.Path)
		query := r.URL.Query()
		assert.Equal(t, "gtx", query.Get("client"))
		assert.Equal(t, "en", query.Get("sl"))
		assert.Equal(t, "zh", query.Get("tl"))

		q := query.Get("q")
		translated := strings.ToUpper(q)
		_, _ = w.Write([]byte(`[[["` + translated + `","` + q + `",null,null,1]],null,"en"]`))
	}))
	defer ts.Close()

	svc := New(config.TranslationConfig{
		Enabled:        true,
		Provider:       "google",
		SourceLanguage: "en",
		TargetLanguage: "zh",
		ApplyToPrimary: true,
		Fields: config.TranslationFieldsConfig{
			Maker: true,
		},
		Google: config.GoogleTranslationConfig{
			Mode:    "free",
			BaseURL: ts.URL,
		},
	})

	movie := &models.Movie{Maker: "studio one"}
	translated, err := svc.TranslateMovie(context.Background(), movie)
	require.NoError(t, err)
	require.NotNil(t, translated)
	assert.Equal(t, "STUDIO ONE", movie.Maker)
	assert.Equal(t, "STUDIO ONE", translated.Maker)
}

func TestParseGoogleFreeResponse(t *testing.T) {
	got, err := parseGoogleFreeResponse([]byte(`[[["Hello","こんにちは",null,null,3]],null,"ja"]`))
	require.NoError(t, err)
	assert.Equal(t, "Hello", got)
}

func TestParseStringArrayPayload_StripsCodeFences(t *testing.T) {
	got, err := parseStringArrayPayload("```json\n[\"a\",\"b\"]\n```")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got)
}

func TestTranslateWithGooglePaidAddsAPIKey(t *testing.T) {
	var capturedQuery url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query()
		response := map[string]any{
			"data": map[string]any{
				"translations": []map[string]any{{"translatedText": "Paid Translation"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer ts.Close()

	svc := New(config.TranslationConfig{
		Enabled:        true,
		Provider:       "google",
		SourceLanguage: "en",
		TargetLanguage: "fr",
		ApplyToPrimary: true,
		Fields: config.TranslationFieldsConfig{
			Series: true,
		},
		Google: config.GoogleTranslationConfig{
			Mode:    "paid",
			BaseURL: ts.URL,
			APIKey:  "GKEY",
		},
	})

	movie := &models.Movie{Series: "My Series"}
	translated, err := svc.TranslateMovie(context.Background(), movie)
	require.NoError(t, err)
	require.NotNil(t, translated)
	assert.Equal(t, "GKEY", capturedQuery.Get("key"))
	assert.Equal(t, "Paid Translation", movie.Series)
}
