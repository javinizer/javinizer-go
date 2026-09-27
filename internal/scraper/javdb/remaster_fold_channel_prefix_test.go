package javdb

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Raw marker-bearing content ids the matcher's tier-2 propagates can carry
// a leading h_/n_ channel prefix: h_003abc00123hd is the ABC-123-HD
// remaster's cid (the 003 channel run riding DMM's h_003 channel). The
// fold key strips that maker letter before the series pins — mirroring the
// DMM/R18 classifiers (underscoreCIDShapeRegex / r18PrefixedCIDRegex) —
// and the catalog digit run behind it then composes with the round-40b
// prefix rules, so the cid folds onto the display listing's identity
// instead of pinning a phantom H series the listing never spells.
func TestFoldRemasterMarkerKeyStripsChannelPrefix(t *testing.T) {
	for _, tc := range []struct {
		name           string
		id             string
		series, number string
		catalogSuffix  string
		marker         string
	}{
		{"h-prefixed channel cid", "h_003abc00123hd", "ABC", "00123", "", "H"},
		{"n-prefixed channel cid", "n_003abc00123hd", "ABC", "00123", "", "H"},
		{"channel cid with AI marker", "h_003abc00123ai", "ABC", "00123", "", "AI"},
		{"channel cid with catalog suffix", "n_118ipx00535zh", "IPX", "00535", "Z", "H"},
		{"channel-prefixed t28 keeps the T28 label", "h_009t28123h", "T28", "123", "", "H"},
		// The catalog digit run behind the channel letter still composes
		// with the round-40b bounds: five-digit runs stay glued to the
		// series, and an ambiguous spelling is not over-stripped.
		{"five-digit channel run keeps its digits", "h_12345abc00123hd", "12345ABC", "00123", "", "H"},
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

// remasterFoldMatchRank: the channel-stripped identity compares against the
// display listing's pinned identity — series, catalog suffix, marker class
// and padding-normalized number — so the raw cid query matches its display
// result in both directions and still rejects every neighboring release.
func TestRemasterFoldMatchRankChannelPrefixedQuery(t *testing.T) {
	target := foldRemasterMarkerKey("h_003abc00123hd")
	assert.Equal(t, idMatchNormalized, remasterFoldMatchRank("ABC-123-HD", target), "the padded cid number folds onto the display number")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("ABC-00123-HD", target))
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("ABC-124-HD", target), "a neighboring number is a different release")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("ABC-123", target), "the base release must not stand in for the remaster")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("ABC-123AI", target), "the AI class is a different release")
	// Reverse direction: a display query must match its raw cid listing.
	display := foldRemasterMarkerKey("ABC-123H")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("h_003abc123hd", display))
	assert.Equal(t, idMatchNormalized, remasterFoldMatchRank("h_003abc00123hd", display))
}

// The finding's scenario: a channel-prefixed marker-bearing content id
// query (h_003abc00123hd, the ABC-123-HD remaster's cid) must resolve
// through the JavDB display listing ABC-123-HD — the same release the
// DMM/R18 classifiers read the cid as — instead of pinning the channel
// letter into the series and missing honestly.
func TestSearchChannelPrefixedQueryMatchesDisplayListing(t *testing.T) {
	for _, id := range []string{"h_003abc00123hd", "n_003abc00123hd"} {
		s := newMarkerTestScraper(map[string]string{
			"https://javdb.test/search?q=" + id + "&f=all": remasterSearchPage("ABC-123-HD"),
			"https://javdb.test/v/abc123hd":                remasterDetailPage("ABC-123-HD"),
		})
		res, err := s.Search(context.Background(), id)
		require.NoError(t, err, "the display listing must satisfy the channel-prefixed cid's folded identity")
		require.NotNil(t, res)
		assert.Equal(t, "ABC-123-HD", res.ID)
	}
}

// The neighboring release must still miss: the channel strip must not
// widen the identity into a series-plus-marker-class-only match, and the
// marker query's single-link fallback stays disabled so the wrong detail
// page is never returned.
func TestSearchChannelPrefixedQueryRejectsNeighborRelease(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=h_003abc00123hd&f=all": remasterSearchPage("ABC-124-HD"),
		"https://javdb.test/v/abc124hd":                     remasterDetailPage("ABC-124-HD"),
	})
	_, err := s.Search(context.Background(), "h_003abc00123hd")
	require.Error(t, err, "a neighboring number is a different release")
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}
