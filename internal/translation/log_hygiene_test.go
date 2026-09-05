package translation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initHygieneLogger points the package logger at a JSON file so tests can
// assert on the exact emitted log content, and restores the default logger.
func initHygieneLogger(t *testing.T) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "translation-hygiene.log")
	require.NoError(t, logging.InitLogger(&logging.Config{Level: "debug", Format: "json", Output: logPath}))
	t.Cleanup(func() { logging.CloseLogger() })
	return logPath
}

// assertHygiene pins the translation-package invariant: neither the rendered
// error nor the captured logs contain request URLs, API keys, source text, or
// provider response transcripts.
func assertHygiene(t *testing.T, logPath string, renderedErr string, secrets ...string) {
	t.Helper()
	raw, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logs := string(raw)

	for _, surface := range []string{renderedErr, logs} {
		assert.NotContains(t, surface, "http://", "no request URLs on errors/logs")
		assert.NotContains(t, surface, "https://", "no request URLs on errors/logs")
		assert.NotContains(t, surface, "key=", "no API key query params on errors/logs")
		for _, secret := range secrets {
			assert.NotContains(t, surface, secret)
		}
	}
}

// unreachableEndpoint refuses connections immediately without DNS lookups.
const unreachableEndpoint = "http://127.0.0.1:1"

func TestLogHygiene_GooglePaidTransportFailure(t *testing.T) {
	logPath := initHygieneLogger(t)
	const sourceText = "SECRET_SOURCE_TEXT_9f3"
	const apiKey = "AKIA_SUPER_SECRET_KEY"

	cfg := Config{
		Enabled: true, Provider: "google", SourceLanguage: "ja", TargetLanguage: "en",
		Fields: fieldsConfig{Title: true},
		Google: googleConfig{Mode: "paid", BaseURL: unreachableEndpoint, APIKey: apiKey},
	}
	s := New(cfg, NewGoogleProvider(cfg, http.DefaultClient))
	_, _, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: sourceText}, "")

	require.Error(t, err)
	assert.Equal(t, TranslationWarningUnavailable, code)
	assertHygiene(t, logPath, err.Error(), sourceText, apiKey)
}

func TestLogHygiene_GoogleFreeTransportFailure(t *testing.T) {
	logPath := initHygieneLogger(t)
	const sourceText = "SECRET_FREE_TEXT_7qz"

	cfg := Config{
		Enabled: true, Provider: "google", SourceLanguage: "ja", TargetLanguage: "en",
		Fields: fieldsConfig{Title: true},
		Google: googleConfig{Mode: "free", BaseURL: unreachableEndpoint},
	}
	s := New(cfg, NewGoogleProvider(cfg, http.DefaultClient))
	_, _, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: sourceText}, "")

	require.Error(t, err)
	assert.Equal(t, TranslationWarningUnavailable, code)
	assertHygiene(t, logPath, err.Error(), sourceText)
}

func TestLogHygiene_DeepLTransportFailure(t *testing.T) {
	logPath := initHygieneLogger(t)
	const sourceText = "SECRET_DEEPL_TEXT_4kz"
	const apiKey = "deepl-secret-auth-key"

	cfg := Config{
		Enabled: true, Provider: "deepl", SourceLanguage: "ja", TargetLanguage: "en",
		Fields: fieldsConfig{Title: true},
		DeepL:  deepLConfig{BaseURL: unreachableEndpoint, APIKey: apiKey},
	}
	s := New(cfg, NewDeepLProvider(cfg, http.DefaultClient))
	_, _, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: sourceText}, "")

	require.Error(t, err)
	assert.Equal(t, TranslationWarningUnavailable, code)
	assertHygiene(t, logPath, err.Error(), sourceText, apiKey)
}

func TestLogHygiene_LLMStatusErrorCarriesNoResponseBody(t *testing.T) {
	logPath := initHygieneLogger(t)
	const providerBody = "PROVIDER_SECRET_RESPONSE_BODY"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(providerBody))
	}))
	defer server.Close()

	cfg := Config{
		Enabled: true, Provider: "openai", SourceLanguage: "ja", TargetLanguage: "en",
		Fields: fieldsConfig{Title: true},
		OpenAI: openAIConfig{BaseURL: server.URL, APIKey: "k", Model: "m"},
	}
	s := New(cfg, NewOpenAIProvider(cfg, server.Client()))
	_, warning, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: "t"}, "")

	require.Error(t, err)
	assert.Equal(t, TranslationWarningServiceError, code)
	assertHygiene(t, logPath, err.Error()+"|"+warning, providerBody)
}

func TestLogHygiene_LLMTransportFailureSharedAndLegacyPipelines(t *testing.T) {
	logPath := initHygieneLogger(t)
	const sourceText = "SECRET_LLM_TEXT_2xw"

	t.Run("shared pipeline", func(t *testing.T) {
		cfg := Config{
			Enabled: true, Provider: "openai", SourceLanguage: "ja", TargetLanguage: "en",
			Fields: fieldsConfig{Title: true},
			OpenAI: openAIConfig{BaseURL: unreachableEndpoint + "/v1", APIKey: "k", Model: "m"},
		}
		s := New(cfg, NewOpenAIProvider(cfg, http.DefaultClient))
		_, _, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: sourceText}, "")
		require.Error(t, err)
		assert.Equal(t, TranslationWarningUnavailable, code,
			"shared-pipeline transport failures are typed provider errors")
		assertHygiene(t, logPath, err.Error(), sourceText)
	})

	t.Run("legacy pipeline", func(t *testing.T) {
		cfg := Config{
			Enabled: true, Provider: "openai-compatible", SourceLanguage: "ja", TargetLanguage: "en",
			Fields: fieldsConfig{Title: true},
			OpenAICompatible: openAICompatibleConfig{
				BaseURL: unreachableEndpoint + "/v1",
				Model:   "m",
			},
		}
		s := New(cfg, NewOpenAICompatibleProvider(cfg, http.DefaultClient))
		_, _, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: sourceText}, "")
		require.Error(t, err)
		assert.Equal(t, TranslationWarningUnavailable, code,
			"both legacy transport branches return typed URL-free provider errors")
		assertHygiene(t, logPath, err.Error(), sourceText)
	})
}

// TestLogHygiene_LLMBodyReadFailures pins truncated/undelivered response
// bodies on both LLM executor pipelines: they classify as unavailable (never
// the unknown fallback) and neither the typed error nor the logs carry the
// raw transport dump.
func TestLogHygiene_LLMBodyReadFailures(t *testing.T) {
	logPath := initHygieneLogger(t)
	const sourceText = "SECRET_BODYREAD_TEXT_5qz"

	t.Run("shared pipeline", func(t *testing.T) {
		cfg := Config{
			Enabled: true, Provider: "openai", SourceLanguage: "ja", TargetLanguage: "en",
			Fields: fieldsConfig{Title: true},
			OpenAI: openAIConfig{BaseURL: "http://provider.invalid/v1", APIKey: "k", Model: "m"},
		}
		s := New(cfg, NewOpenAIProvider(cfg, errBodyReadClient{}))
		_, _, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: sourceText}, "")
		require.Error(t, err)
		assert.Equal(t, TranslationWarningUnavailable, code,
			"shared-pipeline body read failures are typed provider errors -> unavailable")
		assertHygiene(t, logPath, err.Error(), sourceText, bodyReadFailureDump, "SECRET_KEY")
	})

	t.Run("legacy pipeline", func(t *testing.T) {
		cfg := Config{
			Enabled: true, Provider: "openai-compatible", SourceLanguage: "ja", TargetLanguage: "en",
			Fields: fieldsConfig{Title: true},
			OpenAICompatible: openAICompatibleConfig{
				BaseURL: "http://provider.invalid/v1",
				Model:   "m",
			},
		}
		s := New(cfg, NewOpenAICompatibleProvider(cfg, errBodyReadClient{}))
		_, _, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: sourceText}, "")
		require.Error(t, err)
		assert.Equal(t, TranslationWarningUnavailable, code,
			"legacy-pipeline body read failures are typed provider errors -> unavailable")
		assertHygiene(t, logPath, err.Error(), sourceText, bodyReadFailureDump, "SECRET_KEY")
	})
}

// TestLogHygiene_ServiceCallSites pins the service-layer scrubbing: the
// empty-result degraded debug and the all-attempts-failed LLM debug log fixed
// strings plus safe scalars only.
func TestLogHygiene_ServiceCallSites(t *testing.T) {
	logPath := initHygieneLogger(t)
	const sourceText = "SECRET_SERVICE_TEXT_6mk"
	const transcript = "RAW_LLM_TRANSCRIPT_SECRET"

	t.Run("empty result debug keeps no source text", func(t *testing.T) {
		cfg := Config{
			Enabled: true, Provider: "hygiene-stub", SourceLanguage: "ja", TargetLanguage: "en",
			Fields: fieldsConfig{Title: true},
		}
		s := New(cfg, &warningTestProvider{
			name:   "hygiene-stub",
			result: &translationResult{Texts: []string{""}},
		})
		_, warning, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: sourceText}, "")
		require.NoError(t, err)
		assert.Equal(t, TranslationWarningDegraded, code)
		assert.NotEmpty(t, warning)
		assertHygiene(t, logPath, warning, sourceText)
		logs, _ := os.ReadFile(logPath)
		assert.Contains(t, string(logs), "text length=")
	})

	t.Run("all-attempts-failed debug keeps no transcript", func(t *testing.T) {
		cfg := Config{
			Enabled: true, Provider: "hygiene-stub2", SourceLanguage: "ja", TargetLanguage: "en",
			Fields: fieldsConfig{Title: true},
		}
		s := New(cfg, &warningTestProvider{
			name:   "hygiene-stub2",
			result: &translationResult{RawLLM: transcript},
			err:    &translationError{Kind: TranslationErrorParse, Message: "unparseable"},
		})
		_, _, code, err := s.TranslateMovie(context.Background(), &models.Movie{Title: sourceText}, "")
		require.Error(t, err)
		assert.Equal(t, TranslationWarningUnavailable, code)
		assertHygiene(t, logPath, err.Error(), transcript)
		logs, _ := os.ReadFile(logPath)
		assert.Contains(t, string(logs), "last LLM output length=")
	})
}
