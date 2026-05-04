package caribbeancom

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
)

// TestResolveDownloadProxyForHost tests proxy resolution for Caribbeancom hosts
func TestResolveDownloadProxyForHost(t *testing.T) {
	settings := config.ScraperSettings{Enabled: true}
	scraper := New(settings, nil, config.FlareSolverrConfig{})

	tests := []struct {
		name   string
		host   string
		wantOk bool
	}{
		{
			name:   "caribbeancom.com host returns true",
			host:   "www.caribbeancom.com",
			wantOk: true,
		},
		{
			name:   "caribbeancom.com without www",
			host:   "caribbeancom.com",
			wantOk: true,
		},
		{
			name:   "non-caribbeancom host returns false",
			host:   "example.com",
			wantOk: false,
		},
		{
			name:   "empty host returns false",
			host:   "",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := scraper.ResolveDownloadProxyForHost(tt.host)
			assert.Equal(t, tt.wantOk, ok)
		})
	}
}

func mustParseDoc(html string) *goquery.Document {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		panic(err)
	}
	return doc
}

// TestIsMovieDetailPage verifies that the soft-404 page Caribbeancom serves with HTTP 200
// for non-existent movie IDs is correctly rejected.
func TestIsMovieDetailPage(t *testing.T) {
	tests := []struct {
		name string
		html string
		want bool
	}{
		{
			name: "real movie page with movie_id JSON",
			html: `<html><body><script>"movie_id": "120515-039"</script></body></html>`,
			want: true,
		},
		{
			name: "real movie page with #moviepages container",
			html: `<html><body><div id="moviepages"><div class="movie-info"><h1 itemprop="name">Title</h1></div></div></body></html>`,
			want: true,
		},
		{
			name: "real movie page with h1[itemprop='name'] only",
			html: `<html><body><h1 itemprop="name">Some Movie Title</h1></body></html>`,
			want: true,
		},
		{
			name: "soft-404 page with error404-wrap",
			html: `<html><body><div class="error404-wrap"><h1 class="error404-heading">404 NOT FOUND</h1></div></body></html>`,
			want: false,
		},
		{
			name: "empty page",
			html: `<html><body></body></html>`,
			want: false,
		},
		{
			name: "site homepage without movie content",
			html: `<html><head><title>Caribbeancom.com - No.1 Japanese Uncensored Adult Site</title></head><body><div id="header"></div></body></html>`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseDoc(tt.html)
			assert.Equal(t, tt.want, isMovieDetailPage(doc, tt.html))
		})
	}
}
