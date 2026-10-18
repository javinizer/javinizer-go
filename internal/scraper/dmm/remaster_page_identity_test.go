package dmm

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

// The identity guard publishes nothing authoritative to compare for nil
// documents, unparseable display ids, or unparseable queries, and must keep
// the existing behavior in those cases rather than reject the page. A
// nonempty markerless 品番 — plain or E/Z-suffixed — is the exception: on a
// marker-bearing query it names the base release, never the remaster, and
// rejects the page.
func TestPageDisplayIdentityMatchesQueryGuards(t *testing.T) {
	page := func(display string) *goquery.Document {
		t.Helper()
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(
			"<html><body><table><tr><td>品番：</td><td>" + display + "</td></tr></table></body></html>"))
		require.NoError(t, err)
		return doc
	}
	for _, tc := range []struct {
		name  string
		doc   *goquery.Document
		query string
		want  bool
	}{
		{"nil document keeps existing behavior", nil, "DV-818AI", true},
		{"missing display value keeps existing behavior", page("???"), "DV-818AI", true},
		{"unparseable display keeps existing behavior", page("12345"), "DV-818AI", true},
		{"unparseable query keeps existing behavior", page("DV-818-AI"), "remastered", true},
		{"markerless display rejects a marker-bearing query", page("RCT-157"), "RCT-156H", false},
		{"compact markerless display rejects a marker-bearing query", page("RCT157"), "RCT-156H", false},
		{"e-suffixed markerless display rejects a marker-bearing query", page("RCT-157E"), "RCT-156H", false},
		{"z-suffixed markerless display rejects a marker-bearing query", page("RCT-157Z"), "RCT-156H", false},
		{"markerless display with unparseable query keeps existing behavior", page("RCT-157"), "remastered", true},
		{"mismatched release rejects", page("DV-819-AI"), "DV-818AI", false},
		{"matching release accepts", page("DV-818-AI"), "DV-818AI", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, pageDisplayIdentityMatchesQuery(tc.doc, tc.query))
		})
	}
}
