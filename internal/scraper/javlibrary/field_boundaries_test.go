package javlibrary

import (
	"fmt"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Synthetic valid HTML using JavLibrary's section IDs and div/table/td layout.
// Not a captured user/provider page.
func TestParseHTMLFieldBoundaries(t *testing.T) {
	section := func(id, caption, value string) string {
		return fmt.Sprintf(`<div id="%s"><table><tr><td class="header">%s:</td><td class="text">%s</td></tr></table></div>`, id, caption, value)
	}
	anchor := func(value string) string { return `<span><a href="/fixture">` + value + `</a></span>` }
	for _, language := range supportedLanguages {
		caption := map[string]string{"en": "Director", "ja": "監督", "cn": "导演", "tw": "導演"}[language]
		for _, tc := range []struct {
			name, director, maker, label, wantD, wantM, wantL string
			missing                                           bool
		}{
			{name: "empty_director", maker: anchor("Maker A"), label: anchor("Label B"), wantM: "Maker A", wantL: "Label B"},
			{name: "empty_director_and_maker", label: anchor("Label B"), wantL: "Label B"},
			{name: "missing_director", missing: true, maker: anchor("Maker A"), label: anchor("Label B"), wantM: "Maker A", wantL: "Label B"},
			{name: "first_anchor_only", director: anchor("Director C") + anchor("Another Director"), maker: anchor("Maker A") + anchor("Another Maker"), label: anchor("Label B") + anchor("Another Label"), wantD: "Director C", wantM: "Maker A", wantL: "Label B"},
			{name: "populated_control", director: anchor("Director C"), maker: anchor("Maker A"), label: anchor("Label B"), wantD: "Director C", wantM: "Maker A", wantL: "Label B"},
			{name: "empty_anchor_director", director: anchor(""), maker: anchor("Maker A"), label: anchor("Label B"), wantM: "Maker A", wantL: "Label B"},
			{name: "whitespace_anchor_director", director: anchor("   "), maker: anchor("Maker A"), label: anchor("Label B"), wantM: "Maker A", wantL: "Label B"},
			{name: "nested_markup_and_entities", director: anchor("<em>Director</em> &amp; Co"), maker: anchor("Maker &#65;"), label: anchor("Label&nbsp;B"), wantD: "Director & Co", wantM: "Maker A", wantL: "Label\u00a0B"},
			{name: "empty_label_next_genre", director: anchor("Director C"), maker: anchor("Maker A"), wantD: "Director C", wantM: "Maker A"},
		} {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				body := `<html><head><title>TEST-001 Example - JAVLibrary</title></head><body><div id="video_info">`
				if !tc.missing {
					body += section("video_director", caption, tc.director)
				}
				body += section("video_maker", "Maker", tc.maker) + section("video_label", "Label", tc.label) + section("video_genres", "Genres", anchor("Genre D")) + `</div></body></html>`
				s := &scraper{language: language}
				for _, seam := range []string{"raw", "DOM"} {
					t.Run(seam, func(t *testing.T) {
						var result *models.ScraperResult
						var err error
						if seam == "DOM" {
							doc, e := goquery.NewDocumentFromReader(strings.NewReader(body))
							if e != nil {
								t.Fatal(e)
							}
							result, err = s.ParseHTML(doc, "https://example.invalid/"+language+"/?v=test")
						} else {
							result, err = s.ParseHTMLRaw(body, "TEST-001", "https://example.invalid/"+language+"/?v=test", language)
						}
						if err != nil {
							t.Fatal(err)
						}
						t.Logf("Director=%q Maker=%q Label=%q", result.Director, result.Maker, result.Label)
						if result.Director != tc.wantD || result.Maker != tc.wantM || result.Label != tc.wantL {
							t.Errorf("got (%q,%q,%q), want (%q,%q,%q)", result.Director, result.Maker, result.Label, tc.wantD, tc.wantM, tc.wantL)
						}
					})
				}
			})
		}
	}
}

func TestExtractFieldNilDocument(t *testing.T) {
	if got := (&scraper{}).extractField(nil, "video_director"); got != "" {
		t.Fatalf("extractField(nil) = %q, want empty", got)
	}
}
