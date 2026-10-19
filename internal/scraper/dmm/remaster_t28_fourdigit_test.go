package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A prefix-free compact t28 tail with a four-digit number decodes as the
// six-digit T-series release the matcher pins for the same spelling
// (t281234h is T-281234H), so a manual T281234H query searches for the
// T-series release instead of the T28 label's T28-1234H. The boundary rules
// stay pinned: three-digit tails keep the five-digit T-series reading,
// zero-padded five-digit tails keep the T28 label, and separator-bearing and
// catalog-prefixed forms keep their pinned series.
func TestClassifyRemasterQueryPrefixFreeT28FourDigitTail(t *testing.T) {
	for _, q := range []string{"T281234H", "t281234h", "T281234HD"} {
		marker, series, _, isCID := classifyRemasterQuery(q)
		assert.Equal(t, "h", marker, q)
		assert.Equal(t, "t", series, q)
		assert.False(t, isCID, q+" is a display spelling: its four-digit tail is not a raw cid shape")
	}
	assert.Equal(t, "T-281234H", canonicalRemasterDisplayID("T281234H"))
	assert.Equal(t, "T-281234H", canonicalRemasterDisplayID("t281234h"))
	assert.Equal(t, "T-281234H", t28CidDisplayID("t281234h"))
	assert.Contains(t, remasterSearchSpellings("T281234H"), "t-281234-hd")
	for _, s := range remasterSearchSpellings("T281234H") {
		assert.NotContains(t, s, "t28-1234", "the T-series query must not search the T28 label's spelling")
	}

	// The pinned boundary rules stay put.
	marker, series, _, _ := classifyRemasterQuery("t28123h")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "t", series)
	_, series, _, _ = classifyRemasterQuery("t2800123h")
	assert.Equal(t, "t28", series, "the zero-padded five-digit tail stays the T28 label's padded cid")
	_, series, _, _ = classifyRemasterQuery("T28-1234-HD")
	assert.Equal(t, "t28", series, "the separator pins the T28 boundary")
	_, series, _, _ = classifyRemasterQuery("9t281234h")
	assert.Equal(t, "t28", series, "the catalog-prefixed cid stays T28")
	assert.Equal(t, "T28-1234H", canonicalRemasterDisplayID("T28-1234-HD"))
}

// The four-digit extension of the t28 cid convention: the T-series cid
// t281234h is admitted for a series-t query and rejected for a t28 query,
// while the T28 label's own cid spellings for T28-1234H (catalog-prefixed
// 9t281234h, zero-padded t2801234h) never satisfy the T281234H query — the
// two releases are different products whose compact display spellings
// coincide.
func TestT28FourDigitCandidateAdmission(t *testing.T) {
	docFor := func(cid string) *goquery.Document {
		d, err := goquery.NewDocumentFromReader(strings.NewReader(
			"<html><body><a href=\"/mono/dvd/-/detail/=/cid=" + cid + "/\">hit</a></body></html>"))
		require.NoError(t, err)
		return d
	}
	cands := extractRemasterContentIDCandidates(docFor("t281234h"), "t", "h", "")
	require.Len(t, cands, 1, "the T-series cid satisfies the series-t query")
	assert.Equal(t, "t281234h", cands[0].contentID)

	for _, cid := range []string{"9t281234h", "t2801234h"} {
		assert.Empty(t, extractRemasterContentIDCandidates(docFor(cid), "t", "h", ""),
			cid+" is the T28 label's release and must not satisfy the T281234H query")
	}
	assert.Empty(t, extractRemasterContentIDCandidates(docFor("t281234h"), "t28", "h", ""),
		"the T-series cid must not satisfy a T28-1234H query either")

	assert.True(t, anchoredSeriesMatches("t281234h", "t"))
	assert.False(t, anchoredSeriesMatches("t281234h", "t28"))
	assert.False(t, anchoredSeriesMatches("9t281234h", "t"))
	assert.False(t, anchoredSeriesMatches("t2801234h", "t"))
	// The pinned boundary arms stay put.
	assert.True(t, anchoredSeriesMatches("t28123h", "t"))
	assert.False(t, anchoredSeriesMatches("t28123h", "t28"))
	assert.True(t, anchoredSeriesMatches("t2800123h", "t28"))
	assert.True(t, anchoredSeriesMatches("9t28123h", "t28"))
	assert.True(t, anchoredSeriesMatches("9t281234h", "t28"))

	assert.True(t, cachedRemasterIdentityMatches("T281234H", "t281234h", "h", "t", "", false))
	assert.False(t, cachedRemasterIdentityMatches("T281234H", "9t281234h", "h", "t", "", false))
	assert.False(t, cachedRemasterIdentityMatches("T281234H", "t2801234h", "h", "t", "", false))

	// A T28-1234H display and the T281234H query parse to different pinned
	// identities, so verification never lets one stand in for the other.
	cSeries, cNumber, _, _, cOK := displayIdentityTuple("T28-1234-HD")
	require.True(t, cOK)
	qSeries, qNumber, _, _, qOK := displayIdentityTuple("T281234H")
	require.True(t, qOK)
	assert.NotEqual(t, cSeries, qSeries)
	assert.NotEqual(t, cNumber, qNumber)
}

// A manual T281234H query resolves to the T-series release: DMM searches
// the T-series spellings, admits the prefix-free four-digit cid and verifies
// the product page's T-281234H display.
func TestSearchT28FourDigitQueryResolvesTSeriesRelease(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<a href="/mono/dvd/-/detail/=/cid=t281234h/">Remaster</a>`
		}
		return 200, `<html><h1 id="title" class="item">Remaster</h1><table><tr><td>品番：</td><td>T-281234-HD</td></tr></table></html>`
	}})
	result, err := s.Search(context.Background(), "T281234H")
	require.NoError(t, err)
	assert.Equal(t, "t281234h", result.ContentID)
	assert.Equal(t, "T-281234H", result.ID)
}

// The T28 label's T28-1234H must not satisfy the same query: its cid
// spellings (catalog-prefixed, zero-padded) are never admitted for the
// series-t identity, so the search misses honestly instead of publishing the
// other release's metadata.
func TestSearchT28FourDigitQueryMissesT28LabelRelease(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<a href="/mono/dvd/-/detail/=/cid=9t281234h/">Remaster</a>`
		}
		return 200, `<html><h1 id="title" class="item">Remaster</h1><table><tr><td>品番：</td><td>T28-1234-HD</td></tr></table></html>`
	}})
	_, err := s.Search(context.Background(), "T281234H")
	require.Error(t, err, "a T28-1234H result must not satisfy the T281234H query")
}
