package core

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveOriginPort(t *testing.T) {
	for _, tc := range []struct {
		name, origin, port string
	}{
		{"default HTTP", "http://example.test", "80"},
		{"default HTTPS", "https://example.test", "443"},
		{"explicit HTTP", "http://example.test:8080", "8080"},
		{"explicit HTTPS", "https://example.test:8443", "8443"},
		{"no default for a non-HTTP scheme", "ftp://example.test", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := url.Parse(tc.origin)
			require.NoError(t, err)
			require.Equal(t, tc.port, EffectiveOriginPort(parsed))
		})
	}
}
