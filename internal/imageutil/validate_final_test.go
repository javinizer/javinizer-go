package imageutil

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRemoteImageRejectsUnsafeAndDelegatesSafeURL(t *testing.T) {
	require.Error(t, ValidateRemoteImage(t.Context(), "http://127.0.0.1/image.jpg"))

	old := validateRemoteImageWithClient
	t.Cleanup(func() { validateRemoteImageWithClient = old })
	want := errors.New("delegated")
	validateRemoteImageWithClient = func(ctx context.Context, client *http.Client, rawURL, userAgent, referer string) error {
		assert.NotNil(t, ctx)
		assert.NotNil(t, client)
		assert.IsType(t, &pinnedProxyTransport{}, client.Transport)
		assert.Equal(t, "https://example.com/image.jpg", rawURL)
		assert.NotEmpty(t, userAgent)
		assert.Equal(t, "https://example.com/", referer)
		return want
	}
	assert.ErrorIs(t, ValidateRemoteImage(t.Context(), "https://example.com/image.jpg"), want)
}
