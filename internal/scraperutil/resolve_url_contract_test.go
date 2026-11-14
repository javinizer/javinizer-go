package scraperutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveURLReferenceContract(t *testing.T) {
	cases := []struct{ name, base, raw, want string }{
		{"query only", "https://example.com/dir/page?old=1#old", "?new=2", "https://example.com/dir/page?new=2"},
		{"empty query", "https://example.com/dir/page?old=1#old", "?", "https://example.com/dir/page?"},
		{"fragment only", "https://example.com/dir/page?old=1#old", "#new", "https://example.com/dir/page?old=1#new"},
		{"escaped relative", "https://example.com/dir/page", "../a%2Fb?q=%2F#part", "https://example.com/a%2Fb?q=%2F#part"},
		{"absolute other scheme", "https://example.com/dir/page", "ftp://cdn.example.com/a/../b", "ftp://cdn.example.com/b"},
		{"opaque reference", "https://example.com/dir/page", "mailto:user@example.com", "mailto:user@example.com"},
		{"opaque base", "mailto:user@example.com?old=1", "#part", "mailto:user@example.com?old=1#part"},
		{"relative base", "dir/page", "../image.jpg", "/image.jpg"},
		{"empty base", "", "image.jpg", "/image.jpg"},
		{"malformed base", "https://[", "  image.jpg  ", "image.jpg"},
		{"malformed reference", "https://example.com/dir/page", "  bad%zz  ", "bad%zz"},
		{"malformed absolute passthrough", "https://[", "https://other.com/%zz", "https://other.com/%zz"},
		{"protocol relative passthrough", "https://[", "//other.com/%zz", "https://other.com/%zz"},
		{"root path preserves legacy escaping", "https://example.com/dir?q=1#old", "/image.jpg?q=2#part", "https://example.com/image.jpg%3Fq=2%23part"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, ResolveURL(tc.base, tc.raw)) })
	}
}
