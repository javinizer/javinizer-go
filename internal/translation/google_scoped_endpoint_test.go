package translation

import (
	"errors"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestGoogleFreeScopedEndpointConstructionFailure(t *testing.T) {
	// Linux interface names can contain brackets, percent-encoded in a scoped
	// IPv6 URL. Go currently accepts this endpoint but can emit unescaped
	// brackets in the literal when formatting the URL again.
	const endpoint = "http://[fe80::1%25lan%5Blab%5D]:8080"
	parsed, err := url.Parse(endpoint)
	require.NoError(t, err)
	if _, err := http.NewRequestWithContext(t.Context(), http.MethodGet, parsed.String(), nil); err == nil {
		t.Skip("net/http accepts the serialized scoped endpoint; no construction failure to exercise")
	}

	app := config.DefaultConfig(nil, nil)
	app.Metadata.Translation.Enabled = true
	app.Metadata.Translation.Provider = "google"
	app.Metadata.Translation.Google.Mode = models.GoogleModeFree
	app.Metadata.Translation.Google.BaseURL = endpoint
	require.NoError(t, config.ValidateTranslationProvider(app))

	var calls atomic.Int32
	client := &http.Client{Transport: failurePathTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected transport call")
	})}
	provider := NewGoogleProvider(ConfigFromApp(app.Metadata.Translation), client)
	result, err := provider.Translate(t.Context(), "ja", "en", []string{"翻訳の例"})
	require.Nil(t, result)
	var providerErr *translationError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, TranslationErrorProvider, providerErr.Kind)
	require.EqualError(t, err, "google free translation: failed to build request")
	require.Zero(t, calls.Load())
}
