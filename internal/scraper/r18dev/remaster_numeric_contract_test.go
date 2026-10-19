package r18dev

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func numericContractStore(t *testing.T, cids ...string) *r18devdump.Store {
	t.Helper()
	var rows strings.Builder
	for _, cid := range cids {
		fmt.Fprintf(&rows, "%s\t\\N\n", cid)
	}
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" + rows.String() + "\\.\n"
	path := filepath.Join(t.TempDir(), "identity.db")
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{SourceDate: "2026-10-18"})
	require.NoError(t, err)
	store, err := r18devdump.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

func numericContractScraper(lookup models.R18DevDumpLookup, transport *byURLTransport) *scraper {
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.RetryCount = 0
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, lookup)
	s.client.SetRetryCount(0)
	s.client.SetTransport(transport)
	return s
}

func TestRemasterNumericCandidatesPreserveOriginalPriority(t *testing.T) {
	for _, id := range []string{"ABC12345HD", "ABC123456HD", "ABC000123HD", "ABC12345ZH", "ABC123456AI", "ABC01234HD", "1abc123456hd", "h_003abc123456hd", "ABC-123456-HD", "T-28123-Z-HD"} {
		t.Run(id, func(t *testing.T) {
			original := r18devdump.ContentIDCandidatesWithMarker(id)
			candidates := remasterContentIDCandidates(id)
			require.GreaterOrEqual(t, len(candidates), len(original))
			assert.Equal(t, original, candidates[:len(original)])
			seen := map[string]bool{}
			for _, cid := range candidates {
				assert.False(t, seen[cid], "duplicate %s", cid)
				seen[cid] = true
			}
			if isRawRemasterContentIDQuery(id) || strings.Contains(id, "-") {
				assert.Equal(t, original, candidates)
			}
		})
	}
	assert.Contains(t, remasterContentIDCandidates("ABC12345HD"), "1abc12345h")
	assert.Contains(t, remasterContentIDCandidates("ABC123456HD"), "1abc123456h")
	assert.Contains(t, remasterContentIDCandidates("ABC000123HD"), "1abc00123h")
}

func TestSearchCompactNumericDisplayFallbackKeepsLiteralPrecedence(t *testing.T) {
	for _, tc := range []struct{ query, canonical string }{
		{"ABC12345HD", "1abc12345h"}, {"ABC123456HD", "1abc123456h"}, {"ABC000123HD", "1abc00123h"}, {"ABC123456ZHD", "1abc123456zh"},
	} {
		for _, exactPresent := range []bool{false, true} {
			for _, useDump := range []bool{false, true} {
				name := fmt.Sprintf("%s/exact=%t/dump=%t", tc.query, exactPresent, useDump)
				t.Run(name, func(t *testing.T) {
					require.False(t, isRawRemasterContentIDQuery(tc.query))
					raw := strings.ToLower(tc.query)
					cids := []string{tc.canonical}
					want := tc.canonical
					if exactPresent {
						cids = append(cids, raw)
						want = raw
					}
					store := numericContractStore(t, cids...)
					var hits []string
					transport := &byURLTransport{responder: func(_ int, path string) (int, string) {
						hits = append(hits, path)
						for _, cid := range cids {
							if strings.Contains(path, "combined="+cid+"/json") {
								return 200, fmt.Sprintf(`{"content_id":%q,"dvd_id":null,"title_en":%q}`, cid, "Product "+cid)
							}
						}
						return 404, ""
					}}
					var lookup models.R18DevDumpLookup
					if useDump {
						lookup = store
					}
					result, err := numericContractScraper(lookup, transport).Search(context.Background(), tc.query)
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, want, result.ContentID)
					assert.Equal(t, "Product "+want, result.Title)
					if exactPresent && useDump {
						assert.Len(t, hits, 1, "exact dump identity retains the fast path")
					}
					if !exactPresent {
						assert.Contains(t, hits, "/videos/vod/movies/detail/-/combined="+raw+"/json", "literal identity must be probed before display fallback")
					}
				})
			}
		}
	}
}

func TestSearchRawNumericIdentitiesKeepMarkerAndPrefix(t *testing.T) {
	for _, query := range []string{"abc01234hd", "1abc123456hd", "h_003abc123456hd", "1abc123456789012345678901234567890hd"} {
		t.Run(query, func(t *testing.T) {
			require.True(t, isRawRemasterContentIDQuery(query))
			store := numericContractStore(t, query)
			var hits []string
			transport := &byURLTransport{responder: func(_ int, path string) (int, string) {
				hits = append(hits, path)
				return 200, fmt.Sprintf(`{"content_id":%q,"dvd_id":null,"title_en":"Exact identity"}`, query)
			}}
			result, err := numericContractScraper(store, transport).Search(context.Background(), query)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, query, result.ContentID)
			assert.Len(t, hits, 1)
			marker, series := classifyRemaster(query)
			assert.False(t, markerVariationAccept([]byte(fmt.Sprintf(`{"content_id":%q,"dvd_id":null}`, strings.TrimSuffix(query, "hd")+"h")), query, marker, series))
		})
	}
}

func TestRemasterNumericFallbackStillUsesOriginalQueryGuard(t *testing.T) {
	for _, tc := range []struct{ cid, dvd string }{
		{"1abc123457h", "ABC-123457-HD"}, {"1abc123456h", "ABC-123457-HD"}, {"1abc123456ai", "ABC-123456-AI"}, {"1abc123456zh", "ABC-123456-Z-HD"},
	} {
		t.Run(tc.cid+tc.dvd, func(t *testing.T) {
			transport := &byURLTransport{responder: func(_ int, path string) (int, string) {
				if strings.Contains(path, "combined=1abc123456h/json") {
					return 200, fmt.Sprintf(`{"content_id":%q,"dvd_id":%q,"title_en":"Wrong identity"}`, tc.cid, tc.dvd)
				}
				return 404, ""
			}}
			result, err := numericContractScraper(nil, transport).Search(context.Background(), "ABC123456HD")
			require.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

// A partial dump's display candidate cannot prove that an earlier literal
// product is absent from the provider. The HTTP resolver must keep its full
// original-first order rather than trusting the dump's canonical-only row.
func TestSearchIncompleteNumericDumpKeepsLiteralHTTPPriority(t *testing.T) {
	const query = "ABC123456HD"
	const literal = "abc123456hd"
	const canonical = "1abc123456h"
	store := numericContractStore(t, canonical)
	transport := &byURLTransport{responder: func(_ int, path string) (int, string) {
		for _, cid := range []string{literal, canonical} {
			if strings.Contains(path, "combined="+cid+"/json") {
				return 200, fmt.Sprintf(`{"content_id":%q,"dvd_id":null,"title_en":%q}`, cid, "Product "+cid)
			}
		}
		return 404, ""
	}}
	result, err := numericContractScraper(store, transport).Search(context.Background(), query)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, literal, result.ContentID)
	assert.Equal(t, "Product "+literal, result.Title)

	// A third-party dump adapter that supplies only the late display candidate
	// is subject to the same full-order parity gate.
	s := &scraper{dumpLookup: &stubDumpLookup{matches: []models.DumpMatch{{ContentID: canonical}}}}
	full, candidates := s.searchFromDump(context.Background(), query)
	assert.Nil(t, full)
	assert.Empty(t, candidates)
}
