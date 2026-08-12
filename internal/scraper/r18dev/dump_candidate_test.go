package r18dev

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// candidateAPITransport serves one canned combined= JSON response and counts
// requests to the r18.dev API host so tests can prove the dump candidate path
// issues exactly one API call.
type candidateAPITransport struct {
	apiHits int32
	body    string
}

func (ct *candidateAPITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "/videos/vod/movies/detail/") {
		atomic.AddInt32(&ct.apiHits, 1)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(ct.body)),
			Request:    req,
		}, nil
	}
	// CDN URLs (image dimension probing) return 404 fast.
	return &http.Response{
		StatusCode: 404,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

func (ct *candidateAPITransport) count() int { return int(atomic.LoadInt32(&ct.apiHits)) }

const candidateCombinedJSON = `{
  "content_id": "lulu00441",
  "dvd_id": null,
  "title_en": "I Ended Up Living With My Big-assed Female Friend",
  "title_ja": "同居することになった上京デカ尻女友達",
  "release_date": "2026-07-03",
  "runtime_mins": 160,
  "maker_name_en": "LUNATICS"
}`

// TestSearchFromDump_CandidateHitSingleFetch covers the null-dvd_id fast path:
// LookupMovie misses, MatchByDisplayID yields a row, and Search issues exactly
// one r18.dev API request (the resolved combined= URL) with metadata identical
// to what the HTTP multi-probe resolution would produce.
func TestSearchFromDump_CandidateHitSingleFetch(t *testing.T) {
	dump := &stubDumpLookup{
		matches: []models.DumpMatch{{ContentID: "lulu00441", ServiceCode: "digital", ReleaseDate: "2026-07-03"}},
	}
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.RetryCount = 0
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, dump)
	s.client.SetRetryCount(0)
	tr := &candidateAPITransport{body: candidateCombinedJSON}
	s.client.SetTransport(tr)

	result, err := s.Search(context.Background(), "LULU-441")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, tr.count(), "candidate hit must issue exactly one API request")
	assert.Equal(t, "lulu00441", result.ContentID)
	assert.Equal(t, "I Ended Up Living With My Big-assed Female Friend", result.Title)
	assert.Equal(t, "r18dev", result.Source)
}

// TestSearchFromDump_CandidateHitIgnoresDumpRowMetadata proves the candidate
// path never serves dump-row metadata: even a match carrying a DVDID still
// goes through the single combined= fetch.
func TestSearchFromDump_CandidateHitIgnoresDumpRowMetadata(t *testing.T) {
	dump := &stubDumpLookup{
		matches: []models.DumpMatch{{ContentID: "lulu00441", DVDID: "LULU-441"}},
	}
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.RetryCount = 0
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, dump)
	s.client.SetRetryCount(0)
	tr := &candidateAPITransport{body: candidateCombinedJSON}
	s.client.SetTransport(tr)

	result, err := s.Search(context.Background(), "LULU-441")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, tr.count())
}

// byURLTransport answers each detail request via responder(counted, path) so
// candidate-fallthrough tests can shape responses per combined=<content_id>.
type byURLTransport struct {
	hits      int32
	responder func(hit int, path string) (int, string)
}

func (bt *byURLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "/videos/vod/movies/detail/") {
		hit := int(atomic.AddInt32(&bt.hits, 1))
		code, body := bt.responder(hit, req.URL.Path)
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}
	return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

const candidateMonoJSON = `{
  "content_id": "lulu441",
  "dvd_id": "LULU-441",
  "title_en": "Mono Edition Accepted",
  "release_date": "2026-07-07",
  "runtime_mins": 160
}`

func newCandidateScraper(dump *stubDumpLookup, transport http.RoundTripper) *scraper {
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.RetryCount = 0
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, dump)
	s.client.SetRetryCount(0)
	s.client.SetTransport(transport)
	return s
}

// TestSearchFromDump_CandidateFallthrough: a stale first candidate (404) must
// not dead-end the scrape — the next candidate is fetched and wins.
func TestSearchFromDump_CandidateFallthrough(t *testing.T) {
	dump := &stubDumpLookup{matches: []models.DumpMatch{
		{ContentID: "lulu00441", ServiceCode: "digital"},
		{ContentID: "lulu441", ServiceCode: "mono"},
	}}
	tr := &byURLTransport{responder: func(_ int, path string) (int, string) {
		if strings.Contains(path, "combined=lulu00441") {
			return 404, ""
		}
		if strings.Contains(path, "combined=lulu441") {
			return 200, candidateMonoJSON
		}
		return 404, ""
	}}
	s := newCandidateScraper(dump, tr)

	result, err := s.Search(context.Background(), "LULU-441")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 2, int(atomic.LoadInt32(&tr.hits)), "first candidate 404 must fall through to the second")
	assert.Equal(t, "lulu441", result.ContentID)
	assert.Equal(t, "Mono Edition Accepted", result.Title)
}

// TestSearchFromDump_CandidateAllFailFallsBackToResolver: when every dump
// candidate fetch fails, the regular HTTP URL resolver still runs.
func TestSearchFromDump_CandidateAllFailFallsBackToResolver(t *testing.T) {
	dump := &stubDumpLookup{matches: []models.DumpMatch{
		{ContentID: "lulu00441", ServiceCode: "digital"},
		{ContentID: "lulu441", ServiceCode: "mono"},
	}}
	tr := &byURLTransport{responder: func(_ int, _ string) (int, string) { return 404, "" }}
	s := newCandidateScraper(dump, tr)

	_, err := s.Search(context.Background(), "NOPE-999")
	assert.Error(t, err)
	assert.Greater(t, int(atomic.LoadInt32(&tr.hits)), 2, "all-candidate failure must continue to resolver probing")
}

// TestSearchFromDump_CandidateMismatchRejected: a 200 that answers with a
// different content_id is rejected as identity mismatch; the next candidate
// is then fetched and wins.
func TestSearchFromDump_CandidateMismatchRejected(t *testing.T) {
	dump := &stubDumpLookup{matches: []models.DumpMatch{
		{ContentID: "lulu00441", ServiceCode: "digital"},
		{ContentID: "lulu441", ServiceCode: "mono"},
	}}
	tr := &byURLTransport{responder: func(hit int, path string) (int, string) {
		if strings.Contains(path, "combined=lulu00441") && hit == 1 {
			return 200, `{"content_id":"lulu999","title_en":"Wrong Movie"}`
		}
		if strings.Contains(path, "combined=lulu441") {
			return 200, candidateMonoJSON
		}
		return 404, ""
	}}
	s := newCandidateScraper(dump, tr)

	result, err := s.Search(context.Background(), "LULU-441")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 2, int(atomic.LoadInt32(&tr.hits)), "mismatched 200 must fall through to next candidate")
	assert.Equal(t, "lulu441", result.ContentID)
	assert.Equal(t, "Mono Edition Accepted", result.Title)
}

// TestSearchFromDump_CandidateEmptyJSONRejected: an empty ({}) 200 body must
// not be accepted as a candidate result.
func TestSearchFromDump_CandidateEmptyJSONRejected(t *testing.T) {
	dump := &stubDumpLookup{matches: []models.DumpMatch{{ContentID: "lulu00441"}}}
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.RetryCount = 0
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, dump)
	s.client.SetRetryCount(0)
	s.client.SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "combined=lulu00441") {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
		}
		return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}))

	_, err := s.Search(context.Background(), "LULU-441")
	assert.Error(t, err, "empty payload must not be accepted; resolver probes follow and fail")
}

const candidateCanonicalJSON = `{
  "content_id": "118abf030",
  "dvd_id": "ABF-030",
  "title_en": "Canonical Product",
  "release_date": "2024-01-01",
  "runtime_mins": 100
}`

const candidateNonCanonicalJSON = `{
  "content_id": "436abf00030",
  "dvd_id": "ABF-030",
  "title_en": "Mislabeled Compilation",
  "release_date": "2024-01-02",
  "runtime_mins": 100
}`

// TestSearchFromDump_NonCanonicalCandidateNotTrusted covers Codex round-4 P0:
// a dump whose only candidate is a noncanonical prefix (e.g. the ABF-030
// compilation) must NOT win; the flow defers to the HTTP resolver, which
// probes canonical variants first.
func TestSearchFromDump_NonCanonicalCandidateNotTrusted(t *testing.T) {
	dump := &stubDumpLookup{matches: []models.DumpMatch{{ContentID: "436abf00030", ServiceCode: "digital"}}}
	tr := &byURLTransport{responder: func(_ int, path string) (int, string) {
		if strings.Contains(path, "combined=118abf030") {
			return 200, candidateCanonicalJSON
		}
		if strings.Contains(path, "combined=436abf00030") {
			return 200, candidateNonCanonicalJSON
		}
		return 404, ""
	}}
	s := newCandidateScraper(dump, tr)

	result, err := s.Search(context.Background(), "ABF-030")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "118abf030", result.ContentID, "canonical product must win over the dump's lone noncanonical row")
	assert.Equal(t, "Canonical Product", result.Title)
	assert.Greater(t, int(atomic.LoadInt32(&tr.hits)), 1, "resolver probing runs when the canonical candidate is not in the dump")
}

const candidateWrongEditionJSON = `{
  "content_id": "2lulu00441",
  "dvd_id": "LULU-441",
  "title_en": "Wrong Edition Row",
  "release_date": "2026-07-04",
  "runtime_mins": 160
}`

// TestSearchFromDump_GappyCandidatesNotTrusted covers Codex round-5 P0: dump
// matches [lulu00441, 2lulu00441] are NOT a contiguous prefix of the canonical
// expansion ([lulu00441, lulu441, …]); after the first candidate 404s, the
// sparse shortcut must not accept 2lulu00441 — the resolver's probes find the
// intermediate row lulu441 instead.
func TestSearchFromDump_GappyCandidatesNotTrusted(t *testing.T) {
	dump := &stubDumpLookup{matches: []models.DumpMatch{
		{ContentID: "lulu00441", ServiceCode: "digital"},
		{ContentID: "2lulu00441", ServiceCode: "digital"},
	}}
	tr := &byURLTransport{responder: func(_ int, path string) (int, string) {
		switch {
		case strings.Contains(path, "combined=lulu00441"):
			return 404, ""
		case strings.Contains(path, "combined=lulu441"):
			return 200, candidateMonoJSON
		case strings.Contains(path, "combined=2lulu00441"):
			return 200, candidateWrongEditionJSON
		}
		return 404, ""
	}}
	s := newCandidateScraper(dump, tr)

	result, err := s.Search(context.Background(), "LULU-441")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "lulu441", result.ContentID, "sparse dump candidates must not skip the intermediate canonical row")
	assert.Equal(t, "Mono Edition Accepted", result.Title)
}

// TestSearchFromDump_ExactRowInputKeepsResolverParity: a bare unprefixed
// input (both display-id and content-id shaped) follows the HTTP resolver
// order — canonical first — because the exact-row-first store ordering is not
// a canonical prefix of the expansion.
func TestSearchFromDump_ExactRowInputKeepsResolverParity(t *testing.T) {
	dump := &stubDumpLookup{matches: []models.DumpMatch{
		{ContentID: "lulu441", ServiceCode: "mono"},
		{ContentID: "lulu00441", ServiceCode: "digital"},
	}}
	tr := &byURLTransport{responder: func(_ int, path string) (int, string) {
		if strings.Contains(path, "combined=lulu00441") {
			return 200, candidateCombinedJSON
		}
		if strings.Contains(path, "combined=lulu441") {
			return 200, candidateMonoJSON
		}
		return 404, ""
	}}
	s := newCandidateScraper(dump, tr)

	result, err := s.Search(context.Background(), "lulu441")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "lulu00441", result.ContentID, "scraper stays on HTTP-resolver canonical order for ambiguous inputs")
}

// TestSearchFromDump_CandidateLookupCanceled covers the quiet-cancel arm: a
// candidate lookup that dies via context cancellation logs at debug and does
// not report a degraded dump.
func TestSearchFromDump_CandidateLookupCanceled(t *testing.T) {
	dump := &stubDumpLookup{matchErr: context.Canceled}
	s, _ := newScraperWithBlockedHTTP(t, dump)
	result, candidates := s.searchFromDump(context.Background(), "IPX-535")
	assert.Nil(t, result)
	assert.Empty(t, candidates)
}

// TestFetchAndParseCombined_TransportError exercises the shared helper's
// transport-error arm with a nil-returning RoundTripper (resty converts it to
// a request error, never a nil response).
func TestFetchAndParseCombined_TransportError(t *testing.T) {
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.RetryCount = 0
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	s.client.SetRetryCount(0)
	s.client.SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, nil
	}))
	_, err := s.fetchAndParseCombined(context.Background(), "https://example.invalid/x")
	require.Error(t, err)
}

// TestSearchFromDump_EmptyCandidateRowsSkipped covers stub rows lacking a
// ContentID: they are skipped, and a fully-empty candidate list degrades to
// HTTP.
func TestSearchFromDump_EmptyCandidateRowsSkipped(t *testing.T) {
	dump := &stubDumpLookup{matches: []models.DumpMatch{{ContentID: ""}}}
	s, _ := newScraperWithBlockedHTTP(t, dump)
	result, candidates := s.searchFromDump(context.Background(), "IPX-535")
	assert.Nil(t, result)
	assert.Empty(t, candidates, "matches without content_id must not produce candidate URLs")
}

// TestSearchFromDump_CandidateMissFallsBackToResolver keeps the existing
// behavior for truly absent IDs: no candidate URL, probe resolution runs.
func TestSearchFromDump_CandidateMissFallsBackToResolver(t *testing.T) {
	dump := &stubDumpLookup{dvdToContent: map[string]string{}}
	s, rt := newScraperWithBlockedHTTP(t, dump)

	_, err := s.Search(context.Background(), "NOPE-999")
	assert.Error(t, err, "blocked HTTP should fail after probe resolution")
	assert.Greater(t, rt.count(), 0, "true miss must fall back to the HTTP resolver")
}

// TestSearchFromDump_CandidateLookupErrorFallsBack covers a degraded candidate
// lookup (non-miss error): warn + HTTP fallback, no candidate URL.
func TestSearchFromDump_CandidateLookupErrorFallsBack(t *testing.T) {
	dump := &stubDumpLookup{matchErr: errors.New("simulated read failure")}
	s, rt := newScraperWithBlockedHTTP(t, dump)

	result, candidateURLs := s.searchFromDump(context.Background(), "IPX-535")
	assert.Nil(t, result)
	assert.Empty(t, candidateURLs)

	_, err := s.Search(context.Background(), "NOPE-999")
	assert.Error(t, err)
	assert.Greater(t, rt.count(), 0)
}
