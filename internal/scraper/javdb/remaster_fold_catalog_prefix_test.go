package javdb

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Raw marker-bearing DMM content ids the matcher's tier-2 propagates carry a
// leading catalog/channel prefix: 1rct00156h is the RCT-156H remaster's cid
// (the r18.dev content-id prefix lookup maps rct -> prefix "1"). The fold
// key strips that unambiguous 1-4 digit run from the series the same way
// the DMM/R18 classifiers read the spelling (parseRemasterTail /
// r18ParseRemasterTail return the digit-free series group), so the query
// folds onto the display listing's identity instead of pinning a 1RCT
// series no listing spells and missing honestly.
func TestFoldRemasterMarkerKeyStripsCatalogPrefix(t *testing.T) {
	for _, tc := range []struct {
		name           string
		id             string
		series, number string
		catalogSuffix  string
		marker         string
	}{
		{"padded catalog-prefixed cid", "1rct00156h", "RCT", "00156", "", "H"},
		{"unpadded catalog-prefixed cid", "1rct156h", "RCT", "156", "", "H"},
		{"three-digit channel prefix", "118ipx00535h", "IPX", "00535", "", "H"},
		{"four-digit catalog prefix", "5342abc00123h", "ABC", "00123", "", "H"},
		{"catalog suffix rides the strip", "118ipx00535zh", "IPX", "00535", "Z", "H"},
		{"prefixed AI cid keeps its class", "118dv00899ai", "DV", "00899", "", "AI"},
		// Prefix-free compact spellings are unchanged: there is no leading
		// digit run to strip.
		{"prefix-free compact spelling", "rct156h", "RCT", "156", "", "H"},
		{"prefix-free t28 spelling", "t28123h", "T", "28123", "", "H"},
		{"prefix-free AI spelling", "dv00899ai", "DV", "00899", "", "AI"},
		// The t28 boundary rules stay put: catalog digits prefixing a t28
		// cid are maker junk that keeps the T28 label's identity, on every
		// prefix length the cids carry.
		{"single-digit prefixed t28", "9t28123h", "T28", "123", "", "H"},
		{"double-digit prefixed t28", "55t28123h", "T28", "123", "", "H"},
		{"three-digit prefixed t28", "874t28123h", "T28", "123", "", "H"},
		{"four-digit prefixed t28", "1038t28123h", "T28", "123", "", "H"},
		{"four-digit prefix four-digit tail", "1038t281234h", "T28", "1234", "", "H"},
		// Digit runs longer than any known catalog prefix stay glued to the
		// series: an ambiguous spelling must not over-strip.
		{"five-digit run keeps its digits", "12345abc00123h", "12345ABC", "00123", "", "H"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := foldRemasterMarkerKey(tc.id)
			assert.True(t, key.pinned)
			assert.Equal(t, tc.series, key.series)
			assert.Equal(t, tc.number, key.number)
			assert.Equal(t, tc.catalogSuffix, key.catalogSuffix)
			assert.Equal(t, tc.marker, key.marker)
		})
	}
}

// remasterFoldMatchRank: the prefix-stripped identity compares against the
// display listing's pinned identity — series, catalog suffix, marker class
// and padding-normalized number — so the raw cid query matches its display
// result in both directions and still rejects every neighboring release.
func TestRemasterFoldMatchRankCatalogPrefixedQuery(t *testing.T) {
	target := foldRemasterMarkerKey("1rct00156h")
	assert.Equal(t, idMatchNormalized, remasterFoldMatchRank("RCT-156-HD", target), "the padded cid number folds onto the display number")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("RCT-00156-HD", target))
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("RCT-157-HD", target), "a neighboring number is a different release")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("RCT-156AI", target), "the AI class is a different release")
	// Reverse direction: a display query must match its raw cid listing.
	display := foldRemasterMarkerKey("RCT-156H")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("1rct156h", display))
	assert.Equal(t, idMatchNormalized, remasterFoldMatchRank("1rct00156h", display))
	// The t28 boundary rules stay put on both sides.
	t28 := foldRemasterMarkerKey("9t28123h")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("T28-123-HD", t28))
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("T-28123-HD", t28), "the prefixed t28 cid is the T28 label's release, not the T series'")
}

// The finding's scenario: a raw marker-bearing DMM content id query
// (1rct00156h, the RCT-156H remaster's cid) must resolve through the JavDB
// display listing RCT-156-HD — the same release DMM/R18 scrape — instead of
// pinning the catalog digit into the series and missing honestly.
func TestSearchRawContentIDQueryMatchesDisplayListing(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=1rct00156h&f=all": remasterSearchPage("RCT-156-HD"),
		"https://javdb.test/v/rct156hd":                remasterDetailPage("RCT-156-HD"),
	})
	res, err := s.Search(context.Background(), "1rct00156h")
	require.NoError(t, err, "the display listing must satisfy the raw cid's folded identity")
	require.NotNil(t, res)
	assert.Equal(t, "RCT-156-HD", res.ID)
}

// The neighboring release must still miss: the prefix strip must not widen
// the identity into a series-plus-marker-class-only match, and the marker
// query's single-link fallback stays disabled so the wrong detail page is
// never returned.
func TestSearchRawContentIDQueryRejectsNeighborRelease(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=1rct00156h&f=all": remasterSearchPage("RCT-157-HD"),
		"https://javdb.test/v/rct157hd":                remasterDetailPage("RCT-157-HD"),
	})
	_, err := s.Search(context.Background(), "1rct00156h")
	require.Error(t, err, "a neighboring number is a different release")
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// The t28 rule is preserved through the strip: a catalog-prefixed t28 cid
// keeps the T28 label's identity and matches the T28 listing's HD spelling.
func TestSearchCatalogPrefixedT28QueryMatchesT28LabelHDListing(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=9t28123h&f=all": remasterSearchPage("T28-123-HD"),
		"https://javdb.test/v/t28123hd":              remasterDetailPage("T28-123-HD"),
	})
	res, err := s.Search(context.Background(), "9t28123h")
	require.NoError(t, err, "the catalog prefix must keep the T28-series identity")
	require.NotNil(t, res)
	assert.Equal(t, "T28-123-HD", res.ID)
}
