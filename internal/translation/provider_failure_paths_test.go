package translation

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func failurePathProviders(base string, client *http.Client) map[string]TranslatorProvider {
	return map[string]TranslatorProvider{
		"google paid": NewGoogleProvider(Config{Google: googleConfig{Mode: models.GoogleModePaid, APIKey: "fixture-key", BaseURL: base}}, client),
		"google free": NewGoogleProvider(Config{Google: googleConfig{Mode: models.GoogleModeFree, BaseURL: base}}, client),
		"deepl":       NewDeepLProvider(Config{DeepL: deepLConfig{APIKey: "fixture-key", BaseURL: base}}, client),
		"openai":      NewOpenAIProvider(Config{OpenAI: openAIConfig{APIKey: "fixture-key", BaseURL: base, Model: "fixture-model"}}, client),
		"anthropic":   NewAnthropicProvider(Config{Anthropic: anthropicConfig{APIKey: "fixture-key", BaseURL: base, Model: "fixture-model"}}, client),
	}
}

func TestProvidersRejectTruncatedResponseWithoutPartialTranslation(t *testing.T) {
	const privateBody = "private-provider-response"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, privateBody)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 2 * time.Second
	for name, provider := range failurePathProviders(server.URL, client) {
		t.Run(name, func(t *testing.T) {
			result, err := provider.Translate(context.Background(), "ja", "en", []string{"private-source-text"})
			require.Nil(t, result)
			require.Error(t, err)
			var typed *translationError
			require.ErrorAs(t, err, &typed)
			require.Equal(t, TranslationErrorProvider, typed.Kind)
			require.Contains(t, err.Error(), "read failed")
			for _, s := range []string{privateBody, "private-source-text", "fixture-key", server.URL} {
				require.NotContains(t, err.Error(), s)
			}
		})
	}
}

type failurePathTransport func(*http.Request) (*http.Response, error)

func (f failurePathTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProvidersRejectInvalidBaseBeforeTransport(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: failurePathTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, fmt.Errorf("unexpected transport")
	})}
	for name, provider := range failurePathProviders("://invalid", client) {
		t.Run(name, func(t *testing.T) {
			result, err := provider.Translate(context.Background(), "ja", "en", []string{"text"})
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
	require.Zero(t, calls.Load())
}
