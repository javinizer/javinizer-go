package r18dev

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearch_ABF030_PrefersCorrectPrefixOverMislabeledDVDID reproduces a real
// r18.dev data-quality bug: r18.dev has TWO content_ids both labeled
// dvd_id=ABF-030:
//
//   - 118abf030  — the REAL ABF-030 (Prestige, "Naked Housekeeper Staff07", 2023)
//   - 436abf00030 — "How Slow Can you Blow?" (Eiten compilation, 2012), mislabeled
//
// The dvd_id=abf030 endpoint returns 436abf00030 with a NULL dvd_id field. The
// old contentIDCoreMatch fallback accepted it because it only compares
// series+number and ignores the DMM prefix (436 vs 118), so the scraper never
// reached resolveByContentIDVariations, which tries the canonical "118" prefix
// first and would have found the correct 118abf030.
//
// After the fix, the scraper must resolve to 118abf030 (the real movie), not
// 436abf00030 (the mislabeled compilation).
func TestSearch_ABF030_PrefersCorrectPrefixOverMislabeledDVDID(t *testing.T) {
	var combinedHits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case strings.Contains(path, "dvd_id=abf030"):
			// r18.dev's dvd_id endpoint returns the mislabeled compilation with
			// a NULL dvd_id (the server did a fuzzy content_id match).
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"content_id": "436abf00030",
				"dvd_id": null,
				"title": "How Slow Can you Blow?"
			}`))
			return

		case strings.Contains(path, "combined=118abf00030"):
			// 5-digit padded canonical form does not exist on r18.dev.
			combinedHits = append(combinedHits, "118abf00030")
			w.WriteHeader(http.StatusNotFound)
			return

		case strings.Contains(path, "combined=118abf030"):
			// 3-digit padded form — the REAL ABF-030.
			combinedHits = append(combinedHits, "118abf030")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "ABF-030",
				"content_id": "118abf030",
				"title_en": "Naked Housekeeper Staff07",
				"title_ja": "全裸家政婦 Staff07",
				"release_date": "2023-10-06",
				"runtime_mins": 120,
				"maker_name_en": "Prestige",
				"actresses": [],
				"categories": []
			}`))
			return

		case strings.Contains(path, "combined=436abf00030"):
			// The mislabeled compilation also resolves via combined=, but the
			// fix must never reach this URL because 118abf030 is tried first.
			combinedHits = append(combinedHits, "436abf00030")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "ABF-030",
				"content_id": "436abf00030",
				"title": "How Slow Can you Blow?",
				"release_date": "2012-01-20",
				"runtime_mins": 154,
				"maker_name_en": "Eiten",
				"actresses": [],
				"categories": []
			}`))
			return

		case strings.Contains(path, "combined=436abf030"):
			combinedHits = append(combinedHits, "436abf030")
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "ABF-030")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Must resolve to the real ABF-030, not the mislabeled compilation.
	assert.Equal(t, "ABF-030", result.ID)
	assert.Equal(t, "118abf030", result.ContentID)
	assert.Equal(t, "Naked Housekeeper Staff07", result.Title)
	assert.Equal(t, "Prestige", result.Maker)

	// The mislabeled 436abf00030 combined= URL must never be requested — the
	// canonical 118 prefix is tried first and succeeds.
	for _, hit := range combinedHits {
		assert.NotEqual(t, "436abf00030", hit, "must not fetch mislabeled 436abf00030")
	}
}

// TestSearch_FuzzyDVDFallback_WhenVariationsAllFail pins down the Step-3
// fallback in ResolveURL: when the dvd_id= endpoint returns a null dvd_id with
// a core-matching content_id AND no generated content-id variation resolves,
// the scraper must still return the recorded fuzzy result rather than failing.
//
// This guards behavior for series absent from the prefix lookup table, where
// generateContentIDVariations only tries the common prefixes ("" and "1") and
// those forms may not exist on r18.dev. Without this fallback, the fix for
// ABF-030 would regress series that relied on the old fuzzy dvd_id acceptance.
func TestSearch_FuzzyDVDFallback_WhenVariationsAllFail(t *testing.T) {
	// zzqq is deliberately absent from contentIDPrefixLookup so the variation
	// generator only tries the common fallback prefixes.
	if _, ok := contentIDPrefixLookup["zzqq"]; ok {
		t.Fatal("test assumes series 'zzqq' is absent from contentIDPrefixLookup")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case strings.Contains(path, "dvd_id=zzqq042"):
			// Null dvd_id with a core-matching content_id — r18.dev fuzzy match.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"content_id": "7zzqq00042",
				"dvd_id": null
			}`))
			return

		case strings.Contains(path, "combined=7zzqq00042"):
			// The fuzzy dvd_id result resolves via its own combined= URL — this is
			// the Step-3 fallback path.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "ZZQQ-042",
				"content_id": "7zzqq00042",
				"title_en": "Fuzzy Fallback Movie",
				"release_date": "2020-03-15",
				"runtime_mins": 90,
				"actresses": [],
				"categories": []
			}`))
			return

		case strings.Contains(path, "combined="):
			// Every generated variation (zzqq00042, zzqq042, 1zzqq00042, 1zzqq042)
			// does not exist.
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "ZZQQ-042")
	require.NoError(t, err)
	require.NotNil(t, result)

	// The fuzzy dvd_id result (7zzqq00042) is returned via the combined= URL
	// built from the recorded content_id, so the parsed movie carries it.
	assert.Equal(t, "ZZQQ-042", result.ID)
	assert.Equal(t, "7zzqq00042", result.ContentID)
}

// TestSearch_VariationResponseMismatch_IsSkipped verifies that a generated
// content-id variation returning 200 for a DIFFERENT movie (mismatched series)
// is rejected by variationCoreMatches and skipped, so the resolver does not
// depend solely on prefix-table ordering. The correct movie is found at a
// later variation.
//
// This pins down the principal residual risk from the code review: a 200
// response under an earlier prefix slot must not short-circuit resolution when
// its content_id/dvd_id does not core-match the request.
func TestSearch_VariationResponseMismatch_IsSkipped(t *testing.T) {
	// abf has prefixes {"118", "436"} in contentIDPrefixLookup. Variation order:
	//   118abf00030, 118abf030, 436abf00030, 436abf030
	if got := contentIDPrefixLookup["abf"]; len(got) == 0 || got[0] != "118" || got[1] != "436" {
		t.Fatalf("test assumes abf prefixes are {118,436}, got %v", got)
	}

	var combinedHits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case strings.Contains(path, "dvd_id=abf030"):
			// Null dvd_id fuzzy match — would be the old (buggy) immediate accept.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id":"436abf00030","dvd_id":null}`))
			return

		case strings.Contains(path, "combined=118abf00030"):
			combinedHits = append(combinedHits, "118abf00030")
			w.WriteHeader(http.StatusNotFound)
			return

		case strings.Contains(path, "combined=118abf030"):
			// 200 but for a DIFFERENT series (xyz, not abf). Must be skipped.
			combinedHits = append(combinedHits, "118abf030:mismatch")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "XYZ-030",
				"content_id": "118xyz00030",
				"title_en": "Wrong Movie"
			}`))
			return

		case strings.Contains(path, "combined=436abf00030"):
			combinedHits = append(combinedHits, "436abf00030")
			w.WriteHeader(http.StatusNotFound)
			return

		case strings.Contains(path, "combined=436abf030"):
			// The real ABF-030 lives here, after the mismatched 118abf030 is skipped.
			combinedHits = append(combinedHits, "436abf030")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "ABF-030",
				"content_id": "436abf030",
				"title_en": "Real ABF-030",
				"release_date": "2023-10-06",
				"runtime_mins": 120,
				"maker_name_en": "Prestige",
				"actresses": [],
				"categories": []
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "ABF-030")
	require.NoError(t, err)
	require.NotNil(t, result)

	// The mismatched 118abf030 was tried but skipped; 436abf030 was accepted.
	assert.Equal(t, "ABF-030", result.ID)
	assert.Equal(t, "436abf030", result.ContentID)
	assert.Equal(t, "Real ABF-030", result.Title)

	// Confirm the mismatched variation was actually requested (validation runs
	// after the fetch, not before).
	assert.Contains(t, combinedHits, "118abf030:mismatch")
}

// TestSearch_VariationInvalidResponse_ContinuesToNext verifies that a variation
// returning a 200 with an invalid body (malformed JSON, or empty identifying
// fields) is rejected and the resolver continues to a later variation that
// returns the real movie. This complements the helper-level coverage with a
// resolver-level integration check.
func TestSearch_VariationInvalidResponse_ContinuesToNext(t *testing.T) {
	if got := contentIDPrefixLookup["abf"]; len(got) < 2 || got[0] != "118" || got[1] != "436" {
		t.Fatalf("test assumes abf prefixes start with {118,436}, got %v", got)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case strings.Contains(path, "dvd_id=abf030"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id":"436abf00030","dvd_id":null}`))
			return

		case strings.Contains(path, "combined=118abf00030"):
			// Malformed JSON body — must be rejected by variationCoreMatches.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{not valid json`))
			return

		case strings.Contains(path, "combined=118abf030"):
			// Valid JSON but empty identifying fields — must be rejected.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"dvd_id":"","content_id":""}`))
			return

		case strings.Contains(path, "combined=436abf00030"):
			w.WriteHeader(http.StatusNotFound)
			return

		case strings.Contains(path, "combined=436abf030"):
			// The real movie lives after two invalid 200 responses.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "ABF-030",
				"content_id": "436abf030",
				"title_en": "Real ABF-030",
				"release_date": "2023-10-06",
				"runtime_mins": 120,
				"maker_name_en": "Prestige",
				"actresses": [],
				"categories": []
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "ABF-030")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "ABF-030", result.ID)
	assert.Equal(t, "436abf030", result.ContentID)
	assert.Equal(t, "Real ABF-030", result.Title)
}

// TestVariationCoreMatches covers the helper directly: exact dvd_id match,
// content_id core-match, mismatched series, mismatched number, and unparseable
// bodies all behave as expected.
func TestVariationCoreMatches(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		query     string
		wantMatch bool
	}{
		{
			name:      "exact dvd_id match",
			body:      `{"dvd_id":"ABF-030","content_id":"118abf030"}`,
			query:     "abf030",
			wantMatch: true,
		},
		{
			name:      "content_id core match (null dvd_id)",
			body:      `{"dvd_id":null,"content_id":"436abf00030"}`,
			query:     "abf030",
			wantMatch: true,
		},
		{
			name:      "mismatched series",
			body:      `{"dvd_id":"XYZ-030","content_id":"118xyz00030"}`,
			query:     "abf030",
			wantMatch: false,
		},
		{
			name:      "mismatched number",
			body:      `{"dvd_id":"ABF-999","content_id":"118abf00999"}`,
			query:     "abf030",
			wantMatch: false,
		},
		{
			name:      "unparseable body",
			body:      `{not json`,
			query:     "abf030",
			wantMatch: false,
		},
		{
			name:      "empty fields",
			body:      `{"dvd_id":"","content_id":""}`,
			query:     "abf030",
			wantMatch: false,
		},
		{
			// Pins down the OR contract: a mismatched dvd_id is acceptable when
			// the content_id core-matches. r18.dev sometimes returns a null or
			// stale dvd_id alongside the correct content_id.
			name:      "dvd_id mismatch but content_id matches (accept on content_id)",
			body:      `{"dvd_id":"XYZ-030","content_id":"436abf00030"}`,
			query:     "abf030",
			wantMatch: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := variationCoreMatches([]byte(tc.body), tc.query)
			assert.Equal(t, tc.wantMatch, got)
		})
	}
}

// TestSearch_ExactDVDIDMatch_SkipsVariationProbes verifies that when the dvd_id=
// endpoint returns a response whose dvd_id exactly matches the normalized query,
// ResolveURL returns immediately and no content-id variation probes are made.
// Only the final combined= fetch (for the returned content_id) should occur.
func TestSearch_ExactDVDIDMatch_SkipsVariationProbes(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case strings.Contains(path, "dvd_id=ipx535"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"content_id": "1ipx00535",
				"dvd_id": "IPX-535"
			}`))
			return

		case strings.Contains(path, "combined=1ipx00535"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "IPX-535",
				"content_id": "1ipx00535",
				"title_en": "Exact Match Movie",
				"release_date": "2020-08-13",
				"runtime_mins": 120,
				"maker_name_en": "Test Maker",
				"actresses": [],
				"categories": []
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "IPX-535")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, "1ipx00535", result.ContentID)

	var combinedPaths []string
	for _, p := range requestedPaths {
		if strings.Contains(p, "combined=") {
			combinedPaths = append(combinedPaths, p)
		}
	}
	require.Len(t, combinedPaths, 1, "exactly one combined= fetch (the final movie), no variation probes")
	assert.Contains(t, combinedPaths[0], "combined=1ipx00535")
}

// TestSearch_ContextCanceled_StopsFurtherProbes verifies that when the context
// is cancelled during the dvd_id= lookup, no combined= variation URLs are
// requested. The context is cancelled inside the test server handler when the
// dvd_id= path is hit. After cancellation, resolveByContentIDVariations breaks
// immediately and the fuzzy fallback is skipped, so Search fails fast.
func TestSearch_ContextCanceled_StopsFurtherProbes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var combinedHits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case strings.Contains(path, "dvd_id=abf030"):
			cancel()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id":"436abf00030","dvd_id":null}`))
			return

		case strings.Contains(path, "combined="):
			combinedHits = append(combinedHits, path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	_, err := s.Search(ctx, "ABF-030")
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"error should wrap context.Canceled, got: %v", err)

	assert.Empty(t, combinedHits, "no combined= variation should be requested after cancellation")
}

// TestResolveURL_PreCancelledContext_BreaksDVDidLoop covers the ctx.Err()
// check at the top of the dvd_id= loop (ResolveURL line 324). Search itself
// checks ctx.Err() before calling ResolveURL, so going through Search can
// never reach this check with a cancelled context. Calling ResolveURL
// directly bypasses Search's gate and exercises the defense-in-depth break.
func TestResolveURL_PreCancelledContext_BreaksDVDidLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make any HTTP request when context is pre-cancelled")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := newR18TestScraper(server, true, "en")
	resolver := &r18ContentIDResolver{scraper: s}
	url, ok := resolver.ResolveURL(ctx, "ABF-030")
	assert.False(t, ok, "should not resolve when context is pre-cancelled")
	assert.Empty(t, url)
}

// TestResolveURL_FuzzyFallbackSkipped_WhenContextCancelled covers the
// ctx.Err()==nil gate on the fuzzy fallback (ResolveURL line 378). The dvd_id=
// handler cancels the context AND returns a null-dvd_id fuzzy match, so
// fuzzyContentIDURL is set but ctx is cancelled — the fallback must be
// skipped (returns "", false), not used.
func TestResolveURL_FuzzyFallbackSkipped_WhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case strings.Contains(path, "dvd_id=abf030"):
			cancel()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id":"436abf00030","dvd_id":null}`))
			return
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	resolver := &r18ContentIDResolver{scraper: s}
	url, ok := resolver.ResolveURL(ctx, "ABF-030")
	assert.False(t, ok, "should not resolve when context is cancelled before fallback")
	assert.Empty(t, url)
}

// TestSearch_VariationHTML200_ContinuesToNext verifies that a variation
// returning a 200 with Content-Type: text/html is treated as invalid and
// skipped, so the resolver continues to a later variation that returns valid
// JSON core-matching the requested id.
func TestSearch_VariationHTML200_ContinuesToNext(t *testing.T) {
	if got := contentIDPrefixLookup["abf"]; len(got) < 2 || got[0] != "118" || got[1] != "436" {
		t.Fatalf("test assumes abf prefixes start with {118,436}, got %v", got)
	}

	var htmlHit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case strings.Contains(path, "dvd_id=abf030"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id":"436abf00030","dvd_id":null}`))
			return

		case strings.Contains(path, "combined=118abf00030"):
			htmlHit = true
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><body>Not JSON</body></html>`))
			return

		case strings.Contains(path, "combined=118abf030"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"dvd_id": "ABF-030",
				"content_id": "118abf030",
				"title_en": "HTML Skip Test",
				"release_date": "2023-10-06",
				"runtime_mins": 120,
				"maker_name_en": "Prestige",
				"actresses": [],
				"categories": []
			}`))
			return

		case strings.Contains(path, "combined="):
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "ABF-030")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "ABF-030", result.ID)
	assert.Equal(t, "118abf030", result.ContentID)
	assert.True(t, htmlHit, "the HTML 200 variation should have been hit")
}
