package imageutil

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRemoteImageRejectsUnparseableURL(t *testing.T) {
	// A control character in the host fails url.Parse before any resolution.
	err := ValidateRemoteImage(t.Context(), "http://exa"+string(rune(0x7f))+"mple.test/x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid URL")
}
func TestValidateRemoteImageRejectsUnsafeAndDelegatesSafeURL(t *testing.T) {
	require.Error(t, ValidateRemoteImage(t.Context(), "http://127.0.0.1/image.jpg"))

	// Keep the guard's hostname resolution offline: example.com must never
	// be looked up for real (H2).
	defer ssrf.SetLookupIPForTest(func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})()

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
