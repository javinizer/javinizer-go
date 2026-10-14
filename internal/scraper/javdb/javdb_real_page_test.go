package javdb

import (
	"bytes"
	"compress/gzip"
	"os"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseDetailPageRealArchivedDetailPage runs the full detail parser over a
// complete JavDB detail page captured from production (Wayback Machine copy of
// https://javdb.com/v/00Akq, EYAN-172). The page lists one actress plus two male
// co-stars, so it pins both the actress extraction and the male-actor exclusion
// on real markup instead of synthetic fragments.
func TestParseDetailPageRealArchivedDetailPage(t *testing.T) {
	raw, err := os.ReadFile("testdata/real_detail_eyan172.html.gz")
	require.NoError(t, err)

	zr, err := gzip.NewReader(bytes.NewReader(raw))
	require.NoError(t, err)
	defer func() { _ = zr.Close() }()

	doc, err := goquery.NewDocumentFromReader(zr)
	require.NoError(t, err)

	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/00Akq", "EYAN-172")
	require.NoError(t, err)

	assert.Equal(t, "EYAN-172", result.ID)
	require.Len(t, result.Actresses, 1, "got %v", actressNames(result.Actresses))
	assert.Equal(t, "白石みき", result.Actresses[0].JapaneseName)
	assert.Equal(t, "https://c0.jdbstatic.com/avatars/qd/qDQy6.jpg", result.Actresses[0].ThumbURL)

	for _, actress := range result.Actresses {
		assert.NotEqual(t, "マッスル澤野", actress.JapaneseName)
		assert.NotEqual(t, "鮫島", actress.JapaneseName)
	}
}
