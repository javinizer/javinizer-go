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

// The Step-1 fuzzy record gate must bind the core number for H/HD queries:
// a null-dvd_id row for a different release of the same series is stale
// r18.dev fuzzy-match output, never the requested product. AI queries keep
// the marker-based, number-free acceptance because AI content ids diverge
// from display numbers.

// cidMatchesRemasterFuzzyQuery unit coverage: H/HD binds the core number
// (padding-normalized, T/T28 folding consistent with the marker guard),
// while AI stays number-free.
func TestCidMatchesRemasterFuzzyQuery(t *testing.T) {
	cases := []struct {
		name     string
		cid      string
		query    string
		marker   string
		series   string
		expected bool
	}{
		{"h same number", "1rct00156h", "RCT-156H", "h", "rct", true},
		{"h unpadded cid", "1rct156h", "RCT-156H", "h", "rct", true},
		{"h padded query", "1rct00156h", "RCT-00156-HD", "h", "rct", true},
		{"h raw query echoes itself", "1rct00156h", "1rct00156h", "h", "rct", true},
		{"h t-series folded number", "t28123h", "T-28123-HD", "h", "t", true},
		{"h t28-series padded number", "9t2800123h", "T28-123-HD", "h", "t28", true},
		{"h stale different number", "1rct00157h", "RCT-156H", "h", "rct", false},
		{"h markerless cid rejected", "1rct00156", "RCT-156H", "h", "rct", false},
		{"h foreign series rejected", "dv00899h", "RCT-156H", "h", "rct", false},
		{"h unparseable cid rejected", "1abc0012xh", "ABC-012H", "h", "abc", false},
		{"h unparseable query rejected", "1rct00156h", "nonsense", "h", "rct", false},
		// AI content ids diverge from display numbers (dv00899ai is
		// DV-818AI): number-free, marker-based acceptance.
		{"ai divergent number accepted", "dv00899ai", "DV-818AI", "ai", "dv", true},
		{"ai raw query echoes itself", "dv00899ai", "dv00899ai", "ai", "dv", true},
		{"ai wrong marker rejected", "dv00899h", "DV-818AI", "ai", "dv", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected,
				cidMatchesRemasterFuzzyQuery(tc.cid, tc.query, tc.marker, tc.series))
		})
	}
}

// An H/HD query whose dvd_id= lookup returns a stale same-series row with a
// null dvd_id for a DIFFERENT number (1rct00157h for RCT-156H) must not be
// recorded as the fuzzy fallback: release 157's metadata must never be
// published for release 156 when the content-id variations miss.
func TestRemaster_FuzzyHQuery_NumberMismatchNotRecorded(t *testing.T) {
	var staleFetched bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id="):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00157h", "dvd_id": null}`))
			return
		case strings.Contains(path, "combined=1rct00157h"):
			staleFetched = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00157h", "dvd_id": null, "title_en": "Release 157"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "RCT-156H")
	require.Error(t, err, "a stale same-series marker row for a different number must not resolve")
	assert.Nil(t, result)
	assert.False(t, staleFetched, "the stale fuzzy row must never be fetched")
}

// The matching-number row IS recorded as the fuzzy fallback and returned
// when the variations miss: the row's cid uses an uncommon prefix absent
// from the prefix table, so the Step-2 variation probes cannot reach it and
// only the Step-3 fuzzy URL can.
func TestRemaster_FuzzyHQuery_SameNumberRecordedAndReturned(t *testing.T) {
	var fuzzyFetches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id="):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "7zzqq00042h", "dvd_id": null}`))
			return
		case strings.Contains(path, "combined=7zzqq00042h"):
			fuzzyFetches++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "7zzqq00042h", "dvd_id": null, "title_en": "Uncommon prefix remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "ZZQQ-042H")
	require.NoError(t, err, "a null-dvd_id row carrying the query's core number must resolve via the fuzzy fallback")
	require.NotNil(t, result)
	assert.Equal(t, "7zzqq00042h", result.ContentID)
	assert.Equal(t, 1, fuzzyFetches, "the recorded fuzzy URL must be fetched exactly once")
}

// AI queries keep their number-free acceptance for null-dvd_id rows: AI
// content ids diverge from display numbers (dv00899ai is DV-818AI), so the
// marker-based gate must still record and return the row when the
// variations miss (the divergent number is unreachable via Step 2).
func TestRemaster_FuzzyAIQuery_NumberDivergenceStillAccepted(t *testing.T) {
	var fuzzyFetches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id="):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "dv00899ai", "dvd_id": null}`))
			return
		case strings.Contains(path, "combined=dv00899ai"):
			fuzzyFetches++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "dv00899ai", "dvd_id": null, "title_en": "AI Remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "DV-818AI")
	require.NoError(t, err, "AI number divergence must keep the number-free marker acceptance")
	require.NotNil(t, result)
	assert.Equal(t, "dv00899ai", result.ContentID)
	assert.Equal(t, "", result.ID, "null dvd_id with a marker cid leaves the display ID unset")
	assert.Equal(t, 1, fuzzyFetches)
}

// The AI stale-reject side: number-free does not mean marker-free — a
// null-dvd_id row carrying a different folded marker must not be recorded
// for an AI query.
func TestRemaster_FuzzyAIQuery_ForeignMarkerRowNotRecorded(t *testing.T) {
	var staleFetched bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id="):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "dv00899h", "dvd_id": null}`))
			return
		case strings.Contains(path, "combined=dv00899h"):
			staleFetched = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "dv00899h", "dvd_id": null, "title_en": "HD Remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "DV-818AI")
	require.Error(t, err, "an H-marker row must not resolve for an AI query")
	assert.Nil(t, result)
	assert.False(t, staleFetched, "the foreign-marker fuzzy row must never be fetched")
}
