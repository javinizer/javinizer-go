package javdb

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fold key keeps the t28 convention the DMM/R18 identity code applies:
// the series/number boundary is parsed before punctuation is discarded, so
// T-28123H folds to the key T+28123 while T28-123H — and the compact
// t28123h — fold to T28+123, and the compaction-equal spellings of the two
// distinct releases never share a fold key.
func TestFoldRemasterMarkerKeyPinsSeriesBoundary(t *testing.T) {
	for _, tc := range []struct {
		name           string
		id             string
		series, number string
		catalogSuffix  string
		marker         string
		pinned         bool
	}{
		{"T series with separator", "T-28123H", "T", "28123", "", "H", true},
		{"T series HD spelling folds", "T-28123-HD", "T", "28123", "", "H", true},
		{"T28 series with separator", "T28-123H", "T28", "123", "", "H", true},
		{"compact t28 spelling", "t28123h", "T28", "123", "", "H", true},
		{"compact t28 HD spelling", "T28123HD", "T28", "123", "", "H", true},
		{"separator variants pin alike", "T_28123_H", "T", "28123", "", "H", true},
		{"E/Z suffix survives the pin", "IPX-535-ZH-HD", "IPX", "535", "Z", "H", true},
		{"E-suffixed AI keeps its class", "T28-123-E-AI", "T28", "123", "E", "AI", true},
		{"number padding survives the pin", "RCT-0156-HD", "RCT", "0156", "", "H", true},
		{"digit-led series segment", "300MIUM-700H", "300MIUM", "700", "", "H", true},
		{"base release stays unpinned", "RCT-156", "", "", "", "", false},
		{"FC2-PPV grammar stays unpinned", "FC2-PPV-1234567H", "", "", "", "", false},
		{"markerless part letter stays unpinned", "IPX-535A", "", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := foldRemasterMarkerKey(tc.id)
			assert.Equal(t, tc.pinned, key.pinned)
			if !tc.pinned {
				return
			}
			assert.Equal(t, tc.series, key.series)
			assert.Equal(t, tc.number, key.number)
			assert.Equal(t, tc.catalogSuffix, key.catalogSuffix)
			assert.Equal(t, tc.marker, key.marker)
		})
	}
}

// remasterFoldMatchRank: pinned identities compare series, catalog suffix,
// marker class and number directly, so T-28123H and T28-123H — whose compact
// spellings coincide — rank as no match in either direction, while the H/HD
// spellings of one identity fold together and only number padding separates
// the padding-equal rank from the exact one.
func TestRemasterFoldMatchRankPinnedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		candidate string
		target    string
		want      idMatchType
	}{
		{"cross-series compact collision", "T28-123H", "T-28123H", idMatchNone},
		{"cross-series reverse direction", "T-28123H", "T28-123-HD", idMatchNone},
		{"compact t28 form matches its series", "T28123H", "T28-123-HD", idMatchExact},
		{"HD spelling folds into H", "T-28123-HD", "T-28123H", idMatchExact},
		{"padding-equal number", "RCT-0156H", "RCT-156H", idMatchNormalized},
		{"different number stays none", "T28-124H", "T28-123H", idMatchNone},
		{"marker class separates AI", "RCT-156AI", "RCT-156H", idMatchNone},
		{"catalog suffix separates releases", "IPX-535H", "IPX-535ZH", idMatchNone},
		// The fallback comparison's variant rung stays live for unpinned
		// sides; findDetailURLCtx still rejects it for marker queries.
		{"base release stays variant", "RCT-156", "RCT-156H", idMatchVariant},
		{"unpinned target keeps compact comparison", "FC2-PPV-1234567H", "FC2-PPV-1234567H", idMatchExact},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, remasterFoldMatchRank(tc.candidate, foldRemasterMarkerKey(tc.target)))
		})
	}
}

func TestTrimRemasterNumberPadding(t *testing.T) {
	assert.Equal(t, "156", trimRemasterNumberPadding("0156"))
	assert.Equal(t, "28123", trimRemasterNumberPadding("28123"))
	assert.Equal(t, "0", trimRemasterNumberPadding("000"))
}

func TestAllDigits(t *testing.T) {
	assert.True(t, allDigits("28123"))
	assert.True(t, allDigits("0"))
	assert.False(t, allDigits(""))
	assert.False(t, allDigits("PPV535"))
	assert.False(t, allDigits("28 123"))
}

// A marker query for the T series' T-28123H must not resolve through the
// T28 label's T28-123H: the two releases' compact spellings coincide
// (T28123H), but the separator-pinned fold keys differ (T+28123 vs
// T28+123), so the sole search result for the other series ranks as no
// match and the query misses honestly instead of leaking the wrong detail
// page.
func TestSearchMarkerQueryDoesNotCrossTSeriesCollision(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=T-28123H&f=all": remasterSearchPage("T28-123H"),
		"https://javdb.test/v/t28123h":               remasterDetailPage("T28-123H"),
	})
	_, err := s.Search(context.Background(), "T-28123H")
	require.Error(t, err, "a T28-series listing must not satisfy the T-series query")
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// Control: the same separator-pinned identity still resolves when the
// listing spells the HD marker the query folds from.
func TestSearchMarkerQueryPinnedSeriesMatchesHDListing(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=T-28123H&f=all": remasterSearchPage("T-28123-HD"),
		"https://javdb.test/v/t28123hd":              remasterDetailPage("T-28123-HD"),
	})
	res, err := s.Search(context.Background(), "T-28123H")
	require.NoError(t, err, "the same release's HD spelling must match the folded H query")
	require.NotNil(t, res)
	assert.Equal(t, "T-28123-HD", res.ID)
}

// Symmetric direction: the T28 label's folded T28-123-HD query must not
// resolve through the T series' T-28123H listing the compact spelling
// equally names.
func TestSearchMarkerQueryT28FormDoesNotCrossTSeries(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=T28-123-HD&f=all": remasterSearchPage("T-28123H"),
		"https://javdb.test/v/t28123h":                 remasterDetailPage("T-28123H"),
	})
	_, err := s.Search(context.Background(), "T28-123-HD")
	require.Error(t, err, "a T-series listing must not satisfy the T28-series query")
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}
