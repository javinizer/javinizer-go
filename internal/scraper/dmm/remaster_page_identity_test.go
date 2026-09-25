package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Finding probe: a persistent cache maps the AI display query DV-818AI to a
// stale same-series cid whose page 品番 is DV-819-AI; Search must not return
// release 819 metadata for the query.
func TestCachedAIQueryWrongPageNumberRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "DV-818AI", "dv00999ai")
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "cid=dv00999ai") {
			return 200, `<html><body><h1 id="title" class="item">DV-819 AI Remaster</h1>` +
				`<table><tr><td>品番：</td><td>DV-819-AI</td></tr></table></body></html>`
		}
		return 404, ""
	}})
	res, err := s.Search(context.Background(), "DV-818AI")
	require.Error(t, err, "the wrong-release page must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// A matching page (DV-818-AI or DV-818AI) returns as today, with hyphenation
// folded by the identity comparison.
func TestCachedAIQueryMatchingPageReturns(t *testing.T) {
	for _, pageDisplay := range []string{"DV-818-AI", "DV-818AI"} {
		s, _ := newRemasterTestScraper(t)
		s.cacheContentID(context.Background(), "DV-818AI", "dv00899ai")
		s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
			if strings.Contains(u, "cid=dv00899ai") {
				return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1>` +
					`<table><tr><td>品番：</td><td>` + pageDisplay + `</td></tr></table></body></html>`
			}
			return 404, ""
		}})
		res, err := s.Search(context.Background(), "DV-818AI")
		require.NoError(t, err, pageDisplay)
		require.NotNil(t, res, pageDisplay)
		assert.Equal(t, "DV-818AI", res.ID, pageDisplay)
		assert.Equal(t, "dv00899ai", res.ContentID, pageDisplay)
	}
}

// H/HD queries keep working through the cache-hit path: their number
// binding already exists in cachedRemasterIdentityMatches, and a matching
// page 品番 must not be double-rejected here.
func TestCachedHQueryMatchingPageReturns(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "RCT-156H", "1rct00156h")
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "cid=1rct00156h") {
			return 200, `<html><body><h1 id="title" class="item">Remaster</h1>` +
				`<table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table></body></html>`
		}
		return 404, ""
	}})
	res, err := s.Search(context.Background(), "RCT-156H")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "RCT-156H", res.ID)
	assert.Equal(t, "1rct00156h", res.ContentID)
}

// The identity guard publishes nothing authoritative to compare for nil
// documents, unparseable display ids, or unparseable queries, and must keep
// the existing behavior in those cases rather than reject the page.
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
		{"unparseable display keeps existing behavior", page("???"), "DV-818AI", true},
		{"unparseable query keeps existing behavior", page("DV-818-AI"), "remastered", true},
		{"mismatched release rejects", page("DV-819-AI"), "DV-818AI", false},
		{"matching release accepts", page("DV-818-AI"), "DV-818AI", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, pageDisplayIdentityMatchesQuery(tc.doc, tc.query))
		})
	}
}
