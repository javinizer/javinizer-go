package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shared four-digit t28 rule: a prefix-free compact t28 tail with a
// four-digit number reads as the six-digit T-series release the matcher
// pins for the same spelling, so a manual T281234H query classifies as the
// T-series identity (series t, number 281234) instead of the T28 label's
// T28-1234H. The pinned boundary rules stay put: three-digit tails keep the
// five-digit T-series reading, zero-padded five-digit tails keep the T28
// label, and separator-bearing and catalog-prefixed forms keep their series.
func TestR18ParseRemasterTailFourDigitTSeries(t *testing.T) {
	for _, q := range []string{"T281234H", "t281234h", "T281234HD"} {
		series, number, ez, marker, ok := r18ParseRemasterTail(q)
		require.True(t, ok, q)
		assert.Equal(t, "t", series, q)
		assert.Equal(t, "281234", number, q)
		assert.Equal(t, "", ez, q)
		assert.Equal(t, "h", foldMarkerSpelling(marker), q)
	}
	assert.Equal(t, "T-281234H", canonicalRemasterDisplayID("T281234H"))
	assert.Equal(t, []string{"t-281234-hd"}, remasterDisplaySpellings("T281234H"))
	assert.False(t, isRawRemasterContentIDQuery("t281234h"), "the four-digit tail is a display spelling, not a raw cid shape")

	// The pinned boundary rules stay put.
	series, number, _, _, ok := r18ParseRemasterTail("t28123h")
	require.True(t, ok)
	assert.Equal(t, "t", series)
	assert.Equal(t, "28123", number)
	series, number, _, _, ok = r18ParseRemasterTail("t2800123h")
	require.True(t, ok)
	assert.Equal(t, "t28", series, "the zero-padded five-digit tail stays the T28 label's padded cid")
	assert.Equal(t, "00123", number)
	series, _, _, _, _ = r18ParseRemasterTail("T28-1234-HD")
	assert.Equal(t, "t28", series, "the separator pins the T28 boundary")
	series, _, _, _, _ = r18ParseRemasterTail("9t281234h")
	assert.Equal(t, "t28", series, "the catalog-prefixed cid stays T28")
}

// The cid-side arms of the same rule: the T-series cid t281234h satisfies a
// series-t marker query and never a t28 query, while the T28 label's own cid
// spellings for T28-1234H (catalog-prefixed 9t281234h, zero-padded
// t2801234h) never satisfy the T281234H query — the two releases are
// different products whose compact display spellings coincide.
func TestT28FourDigitCIDMarkerMatches(t *testing.T) {
	assert.True(t, cidMatchesMarker("t281234h", "h", "t"), "t281234h is T-281234H: the prefix-free four-digit tail reads as series t")
	assert.False(t, cidMatchesMarker("t281234h", "h", "t28"), "the T-series cid must not satisfy a T28-1234H query")
	assert.False(t, cidMatchesMarker("9t281234h", "h", "t"), "a catalog-prefixed t28 cid is the T28 series")
	assert.False(t, cidMatchesMarker("t2801234h", "h", "t"), "the zero-padded cid is the T28 label's release")
	assert.True(t, cidMatchesMarker("9t281234h", "h", "t28"))
	assert.True(t, cidMatchesMarker("t2801234h", "h", "t28"))
	// The pinned boundary arms stay put.
	assert.True(t, cidMatchesMarker("t28123h", "h", "t"))
	assert.False(t, cidMatchesMarker("t28123h", "h", "t28"))
	assert.True(t, cidMatchesMarker("t2800123h", "h", "t28"))
	assert.True(t, cidMatchesMarker("9t28123h", "h", "t28"))

	assert.True(t, displayIDsMatchByIdentity("T-281234-HD", "T281234H"), "the T-series HD spelling matches the compact query")
	assert.False(t, displayIDsMatchByIdentity("T28-1234-HD", "T281234H"), "a T28-1234H display must not satisfy the T281234H query")
	assert.False(t, displayIDsMatchByIdentity("T-281234-HD", "T28-1234-HD"), "the two releases never cross-match")
}

// A manual T281234H query resolves to the T-series release through the
// display variations: the T-series row's cid (t281234h) satisfies the
// series-t guard and its dvd_id display verifies the pinned identity.
func TestSearchT28FourDigitQueryResolvesTSeriesRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "dvd_id=t-281234-hd"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id":"t281234h","dvd_id":"T-281234-HD","title_en":"T series remaster"}`))
			return
		case strings.Contains(r.URL.Path, "combined=t281234h"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id":"t281234h","dvd_id":"T-281234-HD","title_en":"T series remaster","release_date":"2024-01-01","runtime_mins":120,"actresses":[],"categories":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	res, err := s.Search(context.Background(), "T281234H")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "t281234h", res.ContentID)
	assert.Equal(t, "T-281234H", res.ID)
}

// The T28 label's T28-1234H must not satisfy the same query: its rows are
// rejected by the series guard, so the search misses honestly instead of
// publishing the other release's metadata.
func TestSearchT28FourDigitQueryMissesT28LabelRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "dvd_id=") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id":"9t281234h","dvd_id":"T28-1234-HD","title_en":"T28 label remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	_, err := s.Search(context.Background(), "T281234H")
	require.Error(t, err, "a T28-1234H result must not satisfy the T281234H query")
}
