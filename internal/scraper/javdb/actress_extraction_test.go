package javdb

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
)

func extractionSel(t *testing.T, html string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return doc.Find(".value").First()
}

func actressNames(actresses []models.ActressInfo) []string {
	names := make([]string, 0, len(actresses))
	for _, a := range actresses {
		names = append(names, a.JapaneseName)
	}
	return names
}

func TestExtractActressesGenderEvidenceSemantics(t *testing.T) {
	t.Run("unmarked rows are dropped when the panel marks females", func(t *testing.T) {
		// javdb.com renders every actress as <a class="actor-female"> and leaves
		// male co-stars unmarked, so an unmarked row in such a panel is male.
		html := `<div class="value">
<a href="/actors/aaa">Marked Actress</a><strong class="symbol female">♀</strong>
<a href="/actors/bbb">Unmarked Male</a>
<a href="/actors/ccc">Marked Male</a><strong class="symbol male">♂</strong>
</div>`

		actresses := extractActresses(extractionSel(t, html))
		if len(actresses) != 1 || actresses[0].JapaneseName != "Marked Actress" {
			t.Fatalf("expected only the explicitly female row, got %v", actressNames(actresses))
		}
	})

	t.Run("unmarked rows survive when the panel has no female evidence", func(t *testing.T) {
		// Mirrors that omit markers entirely must not lose actresses; only rows
		// carrying explicit male evidence are dropped here.
		html := `<div class="value">
<a href="/actors/bbb">Unmarked Actress</a>
<a href="/actors/ccc">Marked Male</a><strong class="symbol male">♂</strong>
</div>`

		actresses := extractActresses(extractionSel(t, html))
		if len(actresses) != 1 || actresses[0].JapaneseName != "Unmarked Actress" {
			t.Fatalf("expected the unmarked actress, got %v", actressNames(actresses))
		}
	})
}

func TestExtractActressesNoMarkersDropsMaleHeuristicRows(t *testing.T) {
	html := `<div class="value">
<a class="gender-female" href="/actors/aaa">Female Actress</a>
<a class="gender-male" href="/actors/bbb">Male Actor</a>
</div>`

	actresses := extractActresses(extractionSel(t, html))
	if len(actresses) != 1 || actresses[0].JapaneseName != "Female Actress" {
		t.Fatalf("expected only the female row, got %v", actressNames(actresses))
	}
}

func TestExtractActressesMaleOnlyPanelReturnsNil(t *testing.T) {
	html := `<div class="value">
<a href="/actors/m1">Male One</a><strong class="symbol male">♂</strong>
<a href="/actors/m2">Male Two</a><strong class="symbol male">♂</strong>
</div>`

	if actresses := extractActresses(extractionSel(t, html)); len(actresses) != 0 {
		t.Fatalf("expected no actresses, got %v", actressNames(actresses))
	}
}

func TestExtractActressesAbsoluteAndProtocolRelativeLinksProduceThumbs(t *testing.T) {
	html := `<div class="value">
<a href="https://javdb.com/actors/abc">Absolute</a>
<a href="//javdb.com/actors/def?t=1">Protocol</a>
</div>`

	actresses := extractActresses(extractionSel(t, html))
	if len(actresses) != 2 {
		t.Fatalf("expected 2 actresses, got %v", actressNames(actresses))
	}
	if actresses[0].ThumbURL != "https://c0.jdbstatic.com/avatars/ab/abc.jpg" {
		t.Errorf("absolute link thumb = %q", actresses[0].ThumbURL)
	}
	if actresses[1].ThumbURL != "https://c0.jdbstatic.com/avatars/de/def.jpg" {
		t.Errorf("protocol-relative link thumb = %q", actresses[1].ThumbURL)
	}
}

func TestExtractActressesUsesImageInsideActorLink(t *testing.T) {
	html := `<div class="value"><a href="/actors/xyz"><img data-src="//c0.jdbstatic.com/avatars/xy/xyz.jpg" src="data:image/gif;base64,R0lGOD">Yui</a></div>`

	actresses := extractActresses(extractionSel(t, html))
	if len(actresses) != 1 {
		t.Fatalf("expected 1 actress, got %v", actressNames(actresses))
	}
	if actresses[0].ThumbURL != "https://c0.jdbstatic.com/avatars/xy/xyz.jpg" {
		t.Errorf("img thumb = %q", actresses[0].ThumbURL)
	}
}

func TestExtractActressesPlaceholderImageFallsBackToAvatarPath(t *testing.T) {
	html := `<div class="value"><a href="/actors/xyz"><img src="/img/blank.gif">Yui</a></div>`

	actresses := extractActresses(extractionSel(t, html))
	if len(actresses) != 1 {
		t.Fatalf("expected 1 actress, got %v", actressNames(actresses))
	}
	if actresses[0].ThumbURL != "https://c0.jdbstatic.com/avatars/xy/xyz.jpg" {
		t.Errorf("placeholder fallback thumb = %q", actresses[0].ThumbURL)
	}
}

func TestJavdbActorIDFromLinkVariants(t *testing.T) {
	cases := []struct {
		name string
		href string
		want string
	}{
		{"relative", "/actors/abc", "abc"},
		{"relative with query", "/actors/abc?t=1", "abc"},
		{"relative with trailing slash", "/actors/abc/", "abc"},
		{"absolute", "https://javdb.com/actors/abc", "abc"},
		{"protocol relative", "//javdb.com/actors/abc", "abc"},
		{"mirror host", "https://javdb575.com/actors/abc", "abc"},
		{"not an actor link", "/tags/abc", ""},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<a href="` + tc.href + `">x</a>`))
			if err != nil {
				t.Fatalf("parse html: %v", err)
			}
			if got := javdbActorIDFromLink(doc.Find("a").First()); got != tc.want {
				t.Fatalf("javdbActorIDFromLink(%q) = %q, want %q", tc.href, got, tc.want)
			}
		})
	}
}

func TestJavdbAbsoluteImageURLVariants(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"https", "https://c0.jdbstatic.com/a.jpg", "https://c0.jdbstatic.com/a.jpg"},
		{"protocol relative", "//c0.jdbstatic.com/a.jpg", "https://c0.jdbstatic.com/a.jpg"},
		{"root relative", "/avatars/ab/abc.jpg", "https://javdb.com/avatars/ab/abc.jpg"},
		{"blank gif", "/img/blank.gif", ""},
		{"placeholder", "https://javdb.com/img/placeholder.png", ""},
		{"data uri", "data:image/gif;base64,R0lGOD", ""},
		{"empty", "  ", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := javdbAbsoluteImageURL(tc.raw); got != tc.want {
				t.Fatalf("javdbAbsoluteImageURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
