package translation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// warningTestProvider is a controllable TranslatorProvider for classification tests.
type warningTestProvider struct {
	name   string
	result *translationResult
	err    error
}

func (p *warningTestProvider) Name() string { return p.name }
func (p *warningTestProvider) Translate(_ context.Context, _, _ string, _ []string) (*translationResult, error) {
	return p.result, p.err
}

func TestEffectiveGoogleMode(t *testing.T) {
	tests := []struct {
		name string
		mode models.GoogleMode
		want models.GoogleMode
	}{
		{"unset defaults to free", "", models.GoogleModeFree},
		{"whitespace defaults to free", "   ", models.GoogleModeFree},
		{"paid preserved", "paid", models.GoogleModePaid},
		{"case normalized", "PAID", models.GoogleModePaid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Google: googleConfig{Mode: tt.mode}}
			assert.Equal(t, tt.want, cfg.EffectiveGoogleMode())
		})
	}
}

func TestService_ProviderMode(t *testing.T) {
	t.Run("nil service", func(t *testing.T) {
		var s *Service
		assert.Equal(t, "", s.ProviderMode())
	})

	t.Run("google unset mode defaults to free", func(t *testing.T) {
		s := New(Config{Provider: "google"})
		assert.Equal(t, "free", s.ProviderMode())
	})

	t.Run("google paid", func(t *testing.T) {
		s := New(Config{Provider: "GOOGLE", Google: googleConfig{Mode: "paid"}})
		assert.Equal(t, "paid", s.ProviderMode())
	})

	t.Run("non-google providers have no mode", func(t *testing.T) {
		assert.Equal(t, "", New(Config{Provider: "deepl"}).ProviderMode())
		assert.Equal(t, "", New(Config{Provider: "openai"}).ProviderMode())
	})
}

func TestClassifyTranslationWarning_ModeInMessages(t *testing.T) {
	t.Run("429 names provider and resolved unset mode as free", func(t *testing.T) {
		s := New(Config{Provider: "google"})
		code, message := classifyTranslationWarning(context.Background(),
			normalizeProvider(s.cfg.Provider), s.ProviderMode(),
			&translationError{Kind: TranslationErrorHTTPStatus, StatusCode: 429})
		assert.Equal(t, TranslationWarningRateLimited, code)
		assert.Contains(t, message, "Google Translate (free)")
	})

	t.Run("non-google rates limit without mode suffix", func(t *testing.T) {
		code, message := classifyTranslationWarning(context.Background(), "deepl", "",
			&translationError{Kind: TranslationErrorHTTPStatus, StatusCode: 429})
		assert.Equal(t, TranslationWarningRateLimited, code)
		assert.Contains(t, message, "deepl")
		assert.NotContains(t, message, "()")
	})
}

func TestParseGoogleFreeResponse_TypedParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"invalid json", "not json"},
		{"non-array root", `{"a":1}`},
		{"empty array root", `[]`},
		{"root[0] not an array", `[1]`},
		{"no string parts", `[[[1]]]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseGoogleFreeResponse([]byte(tt.payload))
			require.Error(t, err)
			var te *translationError
			require.True(t, errors.As(err, &te), "expected typed translationError, got %T", err)
			assert.Equal(t, TranslationErrorParse, te.Kind,
				"decode failures classify as parse errors -> unavailable (not unknown)")
		})
	}
}

func TestProviderDecodeFailures_TypedParseErrors(t *testing.T) {
	garbageServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not valid json"))
		}))
	}

	t.Run("google paid decode", func(t *testing.T) {
		server := garbageServer()
		defer server.Close()
		cfg := Config{Google: googleConfig{Mode: "paid", BaseURL: server.URL, APIKey: "k"}}
		p := NewGoogleProvider(cfg, server.Client())
		_, err := p.Translate(context.Background(), "ja", "en", []string{"x"})
		require.Error(t, err)
		var te *translationError
		require.True(t, errors.As(err, &te))
		assert.Equal(t, TranslationErrorParse, te.Kind)
	})

	t.Run("deepl decode", func(t *testing.T) {
		server := garbageServer()
		defer server.Close()
		cfg := Config{DeepL: deepLConfig{BaseURL: server.URL, APIKey: "k"}}
		p := NewDeepLProvider(cfg, server.Client())
		_, err := p.Translate(context.Background(), "ja", "en", []string{"x"})
		require.Error(t, err)
		var te *translationError
		require.True(t, errors.As(err, &te))
		assert.Equal(t, TranslationErrorParse, te.Kind)
	})

	t.Run("openai decode", func(t *testing.T) {
		server := garbageServer()
		defer server.Close()
		cfg := Config{OpenAI: openAIConfig{BaseURL: server.URL, APIKey: "k", Model: "m"}}
		p := NewOpenAIProvider(cfg, server.Client())
		_, err := p.Translate(context.Background(), "ja", "en", []string{"x"})
		require.Error(t, err)
		var te *translationError
		require.True(t, errors.As(err, &te))
		assert.Equal(t, TranslationErrorParse, te.Kind)
	})

	t.Run("anthropic decode", func(t *testing.T) {
		server := garbageServer()
		defer server.Close()
		cfg := Config{Anthropic: anthropicConfig{BaseURL: server.URL, APIKey: "k", Model: "m"}}
		p := NewAnthropicProvider(cfg, server.Client())
		_, err := p.Translate(context.Background(), "ja", "en", []string{"x"})
		require.Error(t, err)
		var te *translationError
		require.True(t, errors.As(err, &te))
		assert.Equal(t, TranslationErrorParse, te.Kind)
	})

	t.Run("openai no choices", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[]}`))
		}))
		defer server.Close()
		cfg := Config{OpenAI: openAIConfig{BaseURL: server.URL, APIKey: "k", Model: "m"}}
		p := NewOpenAIProvider(cfg, server.Client())
		_, err := p.Translate(context.Background(), "ja", "en", []string{"x"})
		require.Error(t, err)
		var te *translationError
		require.True(t, errors.As(err, &te))
		assert.Equal(t, TranslationErrorParse, te.Kind)
	})
}

// TestTranslateMovie_GoogleFreeUndecodablePayloadIsUnavailable covers the spec
// scenario: an undecodable google-free payload is typed TranslationErrorParse
// and classifies as unavailable (never unknown).
func TestTranslateMovie_GoogleFreeUndecodablePayloadIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>Sorry... bot-block page</html>"))
	}))
	defer server.Close()

	cfg := Config{
		Enabled:        true,
		Provider:       "google",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		Fields:         fieldsConfig{Title: true},
		Google:         googleConfig{Mode: "free", BaseURL: server.URL},
	}
	s := New(cfg, NewGoogleProvider(cfg, server.Client()))
	_, warning, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: "タイトル"}, "")
	require.Error(t, err)
	assert.Equal(t, TranslationWarningUnavailable, code)
	assert.NotEmpty(t, warning)
	assert.NotContains(t, warning, "rate limited",
		"decode failures must NOT reuse rate-limit remediation copy")
}

// TestTranslateMovie_TypedCountMismatchIsUnavailable covers the typed side of
// the count-mismatch dual attribution: the service-level typed
// TranslationErrorCountMismatch classifies as unavailable.
func TestTranslateMovie_TypedCountMismatchIsUnavailable(t *testing.T) {
	cfg := Config{
		Enabled:        true,
		Provider:       "test-count",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		Fields:         fieldsConfig{Title: true, Description: true},
	}
	s := New(cfg, &warningTestProvider{
		name:   "test-count",
		result: &translationResult{Texts: []string{"only-one"}},
	})
	movie := &models.Movie{Title: "タイトル", Description: "説明"}
	_, warning, code, err := s.TranslateMovie(context.Background(), movie, "")
	require.Error(t, err)
	assert.Equal(t, TranslationWarningUnavailable, code,
		"typed count mismatch classifies as unavailable")
	assert.NotEmpty(t, warning)
}

// TestTranslateMovie_GracefulNoWarningPaths pins the no-warning surfaces: fully
// successful translation and disabled/skipped paths carry neither warning nor code.
func TestTranslateMovie_GracefulNoWarningPaths(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		s := New(Config{Enabled: false})
		_, warning, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: "x"}, "")
		require.NoError(t, err)
		assert.Empty(t, warning)
		assert.Empty(t, string(code))
	})

	t.Run("same source and target", func(t *testing.T) {
		s := New(Config{Enabled: true, Provider: "google", SourceLanguage: "en", TargetLanguage: "en"})
		_, warning, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: "x"}, "")
		require.NoError(t, err)
		assert.Empty(t, warning)
		assert.Empty(t, string(code))
	})
}
