package javdb

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The four-digit extension of the shared t28 rule: a genuinely compact,
// prefix-free t28 tail with a four-digit number folds to the six-digit
// T-series identity the matcher pins for the same spelling (t281234h is
// T+281234), so a manual T281234H query targets the T-series release
// instead of the T28 label's T28-1234H. The pinned boundary rules stay put.
func TestFoldRemasterMarkerKeyFourDigitTSeries(t *testing.T) {
	for _, tc := range []struct {
		name           string
		id             string
		series, number string
	}{
		{"compact four-digit tail", "t281234h", "T", "281234"},
		{"uppercase four-digit tail", "T281234H", "T", "281234"},
		{"four-digit HD spelling", "T281234HD", "T", "281234"},
		{"three-digit tail stays T series", "t28123h", "T", "28123"},
		{"zero-padded five-digit tail stays T28", "t2800123h", "T28", "00123"},
		{"catalog-prefixed four-digit tail stays T28", "9t281234h", "T28", "1234"},
		{"double-digit catalog prefix stays T28", "55t281234h", "T28", "1234"},
		{"zero-padded four-digit-run cid stays T28", "t2801234h", "T28", "01234"},
		{"separator-bearing four-digit tail stays T28", "T28-1234-HD", "T28", "1234"},
		{"separator-pinned T series", "T-281234-HD", "T", "281234"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := foldRemasterMarkerKey(tc.id)
			assert.True(t, key.pinned)
			assert.Equal(t, tc.series, key.series)
			assert.Equal(t, tc.number, key.number)
			assert.Equal(t, "H", key.marker)
		})
	}
}

// The two releases whose compact spellings coincide never share a fold key:
// a T28-1234H listing must not satisfy a T281234H query and vice versa,
// while the T-series release's own HD spelling still folds to the same
// identity.
func TestRemasterFoldMatchRankFourDigitTail(t *testing.T) {
	target := foldRemasterMarkerKey("T281234H")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("T-281234-HD", target))
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("t281234h", target))
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("T28-1234H", target), "a T28-1234H result must not satisfy the T281234H query")
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("9t281234h", target))
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("t2801234h", target))
	// The reverse direction: the T28 label's query keeps its own identity.
	t28Target := foldRemasterMarkerKey("T28-1234-HD")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("T28-1234H", t28Target))
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("T-281234H", t28Target))
	// The pinned three-digit boundary stays put.
	threeTarget := foldRemasterMarkerKey("t28123h")
	assert.Equal(t, idMatchExact, remasterFoldMatchRank("T-28123-HD", threeTarget))
	assert.Equal(t, idMatchNone, remasterFoldMatchRank("T28-123H", threeTarget))
}

// A manual compact t281234h query follows the shared four-digit t28 rule:
// the T28 label's T28-1234H — whose compact spelling coincides — must not
// satisfy it, and the query misses honestly instead of leaking the other
// series' detail page.
func TestSearchCompactT28FourDigitQueryMissesT28LabelRelease(t *testing.T) {
	// The /v/t281234h video-code URL stays unregistered on purpose: the
	// direct-code shortcut would otherwise bypass the search matching, and
	// an honest miss must come from the fold key (a broken fold would rank
	// the T28 listing, fetch this unregistered detail URL and fail with a
	// non-NotFound error instead).
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=t281234h&f=all": remasterSearchPage("T28-1234H"),
	})
	_, err := s.Search(context.Background(), "t281234h")
	require.Error(t, err, "a T28-label listing must not satisfy the T-series compact query")
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// Control: the same compact query resolves when the listing is the T-series
// release it names, spelled with the HD marker the query's folded H bridges.
func TestSearchCompactT28FourDigitQueryMatchesTSeriesRelease(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=t281234h&f=all": remasterSearchPage("T-281234-HD"),
		"https://javdb.test/v/t281234hd":             remasterDetailPage("T-281234-HD"),
	})
	res, err := s.Search(context.Background(), "t281234h")
	require.NoError(t, err, "the T-series HD spelling must match the compact query's folded H")
	require.NotNil(t, res)
	assert.Equal(t, "T-281234-HD", res.ID)
}
