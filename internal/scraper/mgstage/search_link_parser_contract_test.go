package mgstage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Exercise real HTML parsing, not manufactured selections or attribute hooks.
// This is a pure parser contract, not a live-provider transport test.
func TestSearchLinkParserPreservesFirstUsableMatchingProduct(t *testing.T) {
	const good = `/product/product_detail/259LUXU-1806/`
	for _, tc := range []struct{ name, html, want string }{
		{"missing empty and unrelated links", `<a>missing</a><a href="">empty</a><a href="/other">other</a>`, ""},
		{"wrong identifier and malformed product path", `<a href="/product/product_detail/OTHER-001/">wrong</a><a href="/product/product_detail/">incomplete</a>`, ""},
		{"normalized relative match", `<a href="` + good + `">good</a>`, baseURL + good},
		{"absolute match", `<a href="https://www.mgstage.com` + good + `">good</a>`, baseURL + good},
		{"first match wins", `<a href="` + good + `?first=1">first</a><a href="` + good + `?second=1">second</a>`, baseURL + good + "?first=1"},
		{"ordinary duplicate keeps empty first attribute", `<a href="" href="` + good + `">ignored</a>`, ""},
		{"SVG empty href is not usable despite matching xlink", `<svg><a href="" xlink:href="` + good + `">ignored</a></svg><a href="` + good + `">good</a>`, baseURL + good},
		{"SVG xlink alone remains usable", `<svg><a xlink:href="` + good + `">good</a></svg>`, baseURL + good},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &scraper{}
			require.Equal(t, tc.want, s.findProductInSearchResults(tc.html, "259luxu1806"))
			// A prior match or malformed input must not persist across parses.
			require.Empty(t, s.findProductInSearchResults(`<html><body>empty</body></html>`, "259luxu1806"))
		})
	}
}
