package jav321

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestActressHTMLEntitiesPublicPaths(t *testing.T) {
	aliceBob := []models.ActressInfo{{FirstName: "Alice"}, {FirstName: "Bob"}}
	for _, tc := range []struct {
		name    string
		section string
		want    []models.ActressInfo
	}{
		{name: "plaintext_named_ampersand", section: "Alice &amp; Bob", want: aliceBob},
		{name: "plaintext_raw_ampersand", section: "Alice & Bob", want: aliceBob},
		{name: "plaintext_decimal_ampersand", section: "Alice &#38; Bob", want: aliceBob},
		{name: "plaintext_hex_ampersand", section: "Alice &#x26; Bob", want: aliceBob},
		{name: "plaintext_entity_names", section: "Zo&euml; / Ren&#233;e", want: []models.ActressInfo{{FirstName: "Zoë"}, {FirstName: "Renée"}}},
		{name: "plaintext_nonbreaking_space", section: "Alice&nbsp;Bob", want: aliceBob},
		{name: "plaintext_delimiters", section: "Alice,Bob、Alice/Bob|Alice・Bob\tAlice\nBob", want: aliceBob},
		{name: "plaintext_markup", section: "<span>Alice</span> &amp; <em>Bob</em>", want: aliceBob},
		{name: "plaintext_escaped_literal_tags", section: "&lt;Alice&gt; / &lt;Bob&gt;", want: []models.ActressInfo{{FirstName: "<Alice>"}, {FirstName: "<Bob>"}}},
		{name: "plaintext_entity_deduplication", section: "Zo&euml; / Zoë / Zo&#235;", want: []models.ActressInfo{{FirstName: "Zoë"}}},
		{name: "linked_ampersand_separator", section: `<a href="/actor/alice">Alice</a> &amp; <a href="/actor/bob">Bob</a>`, want: aliceBob},
		{name: "linked_ampersand_in_name", section: `<a href="/actor/duo">Alice &amp; Bob</a>`, want: []models.ActressInfo{{FirstName: "Alice", LastName: "& Bob"}}},
		{name: "linked_multiword_name", section: `<a href="/actor/alice">Alice&nbsp;Smith</a> / <a href="/actor/bob">Bob Jones</a>`, want: []models.ActressInfo{{FirstName: "Alice", LastName: "Smith"}, {FirstName: "Bob", LastName: "Jones"}}},
		{name: "linked_escaped_literal_tags", section: `<a href="/actor/alice"><span>&lt;Alice&gt;</span></a>`, want: []models.ActressInfo{{FirstName: "<Alice>"}}},
		{name: "linked_decode_once", section: `<a href="/actor/literal">&amp;lt;Alice&amp;gt;</a>`, want: []models.ActressInfo{{FirstName: "&lt;Alice&gt;"}}},
		{name: "linked_entity_deduplication", section: `<a>Zo&euml;</a> / <a>Zoë</a> / <a>Zo&#235;</a>`, want: []models.ActressInfo{{FirstName: "Zoë"}}},
		{name: "linked_names_take_precedence", section: `<a>Alice</a> &amp; Bob`, want: []models.ActressInfo{{FirstName: "Alice"}}},
		{name: "empty_entity_whitespace", section: "&nbsp; / &amp;", want: []models.ActressInfo{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const sourceURL = "https://jp.jav321.com/video/abc00123"
			page := `<html><head><title>Fixture title</title></head><body><b>品番</b>: ABC-123<br><b>出演者</b>: ` + tc.section + `<br></body></html>`
			checkResult := func(t *testing.T, result *models.ScraperResult) {
				t.Helper()
				require.NotNil(t, result)
				require.Equal(t, "ABC-123", result.ID)
				require.Equal(t, "jav321", result.Source)
				require.Equal(t, sourceURL, result.SourceURL)
				require.Equal(t, tc.want, result.Actresses)
			}
			t.Run("ParseHTML", func(t *testing.T) {
				doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
				require.NoError(t, err)
				checkResult(t, ParseHTML(doc, sourceURL))
			})
			t.Run("ScrapeURL", func(t *testing.T) {
				calls := 0
				s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
				t.Cleanup(func() { require.NoError(t, s.Close()) })
				s.client.SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					require.Equal(t, http.MethodGet, req.Method)
					require.Equal(t, sourceURL, req.URL.String())
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
						Request:    req,
						Body:       io.NopCloser(strings.NewReader(page)),
					}, nil
				}))
				result, err := s.ScrapeURL(context.Background(), sourceURL)
				require.NoError(t, err)
				require.Equal(t, 1, calls)
				checkResult(t, result)
			})
		})
	}
}
