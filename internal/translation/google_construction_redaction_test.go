package translation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type rejectingConstructionTransport struct{ calls int }

func (r *rejectingConstructionTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	return nil, fmt.Errorf("unexpected request")
}

func TestGooglePaidMalformedBaseDoesNotExposeAPIKey(t *testing.T) {
	transport := &rejectingConstructionTransport{}
	cfg := Config{Google: googleConfig{Mode: models.GoogleModePaid, APIKey: "sensitive-fixture-key", BaseURL: "://invalid"}}
	provider := NewGoogleProvider(cfg, &http.Client{Transport: transport})
	result, err := provider.Translate(context.Background(), "ja", "en", []string{"private-text"})
	require.Nil(t, result)
	require.Error(t, err)
	require.Zero(t, transport.calls)
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		for _, secret := range []string{"sensitive-fixture-key", "private-text", "://invalid"} {
			require.NotContains(t, cause.Error(), secret)
		}
	}
	var typed *translationError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, TranslationErrorProvider, typed.Kind)
	require.EqualError(t, err, "google paid translation request construction failed")
	require.Nil(t, errors.Unwrap(err))
}
