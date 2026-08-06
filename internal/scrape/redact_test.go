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

func TestRedactSourceURL_NonstandardSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"X-Amz-Signature and Credential stripped", "https://cdn.example.com/cover.jpg?X-Amz-Signature=abc&X-Amz-Credential=def", "https://cdn.example.com/cover.jpg"},
		{"auth_token stripped, id preserved", "https://example.com/v/123?auth_token=secret&id=456", "https://example.com/v/123?id=456"},
		{"session_id stripped, sn preserved", "https://example.com/v/123?session_id=x&sn=IPX-123", "https://example.com/v/123?sn=IPX-123"},
		{"token stripped, id preserved", "https://example.com/v/123?id=456&token=secret", "https://example.com/v/123?id=456"},
		{"token stripped, v preserved", "https://www.javlibrary.com/en/?v=javmeABCDE&token=x", "https://www.javlibrary.com/en/?v=javmeABCDE"},
		{"session_id stripped, sn preserved (jav321)", "https://jp.jav321.com/search?sn=IPX-123&session_id=x", "https://jp.jav321.com/search?sn=IPX-123"},
		{"api-key stripped, id preserved", "https://example.com/v/123?api-key=secret&id=456", "https://example.com/v/123?id=456"},
		{"access_key stripped", "https://example.com/v/123?access_key=secret", "https://example.com/v/123"},
		{"private_key stripped", "https://example.com/v/123?private_key=secret", "https://example.com/v/123"},
		{"keyword preserved (non-secret)", "https://www.javlibrary.com/en/vl_searchbyid.php?keyword=IPX-123", "https://www.javlibrary.com/en/vl_searchbyid.php?keyword=IPX-123"},
		{"jwt stripped", "https://example.com/v/123?jwt=eyJhb...&id=456", "https://example.com/v/123?id=456"},
		{"bearer stripped", "https://example.com/v/123?bearer=xyz", "https://example.com/v/123"},
		{"hmac stripped", "https://example.com/v/123?hmac=signature&id=456", "https://example.com/v/123?id=456"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, RedactSourceURL(c.in))
		})
	}
}
