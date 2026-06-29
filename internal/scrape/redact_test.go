package scrape

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactURLQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"http with query stripped", "https://example.com/v/123?token=secret", "https://example.com/v/123"},
		{"http with query+fragment stripped", "https://example.com/v/123?token=secret#frag", "https://example.com/v/123"},
		{"http no query unchanged", "https://example.com/v/123", "https://example.com/v/123"},
		{"query-only host stripped", "https://example.com?token=x", "https://example.com"},
		{"plain ID unchanged (no scheme)", "IPX-123", "IPX-123"},
		{"plain ID with question mark unchanged (no scheme)", "ABC-?123", "ABC-?123"},
		{"empty unchanged", "", ""},
		{"userinfo stripped", "https://user:pass@example.com/v/123?token=secret", "https://example.com/v/123"},
		{"userinfo stripped even without query", "https://user:pass@example.com/v/123", "https://example.com/v/123"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, RedactURLQuery(c.in))
		})
	}
}
