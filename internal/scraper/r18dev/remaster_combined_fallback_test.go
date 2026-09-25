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

// The combined= fallback in Search (every resolver variation missed, so the
// normalized combined=<id> URL is fetched by fetchAndParseCombined) must bind
// H/HD responses to the query's core number: a stale null-dvd_id row for a
// different release of the same series (1rct00157h for RCT-156H) is r18.dev
// fuzzy-match output, never the requested product. AI queries keep the
// number-free marker acceptance because AI content ids diverge from display
// numbers by design (dv00899ai is DV-818AI).

// Unit coverage for the null-dvd_id branch of markerVariationAccept: H/HD
// rows bind the query's core number (padding-normalized, T/T28 folding
// consistent with the marker guard), while AI stays number-free.
func TestMarkerVariationAccept_CombinedNullDVDIDNumberBinding(t *testing.T) {
	// H/HD: the row's cid core number must equal the query's number.
	assert.False(t, markerVariationAccept([]byte(`{"content_id":"1rct00157h","dvd_id":null}`), "RCT-156H", "h", "rct"),
		"a null-dvd_id row for a different release must be rejected")
	assert.True(t, markerVariationAccept([]byte(`{"content_id":"1rct00156h","dvd_id":null}`), "RCT-156H", "h", "rct"))
	assert.True(t, markerVariationAccept([]byte(`{"content_id":"1rct156h","dvd_id":null}`), "RCT-00156-HD", "h", "rct"),
		"padding differences must not reject the matching release")
	// AI: number-free acceptance retained — cid numbers diverge by design.
	assert.True(t, markerVariationAccept([]byte(`{"content_id":"dv00899ai","dvd_id":null}`), "DV-818AI", "ai", "dv"))
}

// An H/HD query whose every resolver variation misses and whose combined=
// fallback serves a stale null-dvd_id row for a DIFFERENT number (1rct00157h
// for RCT-156H) must be rejected: release 157 must never be published for
// release 156.
func TestRemaster_CombinedFallback_HQuery_NumberMismatchRejected(t *testing.T) {
	var combinedFetches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "combined=rct156h/json") {
			combinedFetches++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00157h", "dvd_id": null, "title_en": "Release 157"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "RCT-156H")
	require.Error(t, err, "a stale same-series null-dvd_id row for a different number must not resolve")
	assert.Nil(t, result)
	assert.Greater(t, combinedFetches, 0, "the combined= fallback must have been fetched and rejected by the guard")
}

// The matching-number row (1rct00156h) IS accepted through the combined=
// path when the variations miss: the row is only served on the normalized
// fallback URL, so neither the dvd_id lookups nor the content-id variation
// probes can reach it.
func TestRemaster_CombinedFallback_HQuery_SameNumberAccepted(t *testing.T) {
	var combinedFetches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "combined=rct156h/json") {
			combinedFetches++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156h", "dvd_id": null, "title_en": "Release 156"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "RCT-156H")
	require.NoError(t, err, "a null-dvd_id row carrying the query's core number must resolve via the combined= fallback")
	require.NotNil(t, result)
	assert.Equal(t, "1rct00156h", result.ContentID)
	assert.Equal(t, "", result.ID, "null dvd_id with a marker cid leaves the display ID unset")
	assert.Greater(t, combinedFetches, 0)
}

// AI queries keep their number-free acceptance on the combined= fallback:
// the served row's cid number diverges from the display number (dv00899ai
// is DV-818AI), which the variations cannot reach, so only the number-free
// marker gate can accept it.
func TestRemaster_CombinedFallback_AIQuery_NumberFreeAcceptance(t *testing.T) {
	var combinedFetches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "combined=dv818ai/json") {
			combinedFetches++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "dv00899ai", "dvd_id": null, "title_en": "AI Remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "DV-818AI")
	require.NoError(t, err, "AI number divergence must keep the number-free marker acceptance on the combined= path")
	require.NotNil(t, result)
	assert.Equal(t, "dv00899ai", result.ContentID)
	assert.Equal(t, "", result.ID, "null dvd_id with a marker cid leaves the display ID unset")
	assert.Greater(t, combinedFetches, 0)
}
