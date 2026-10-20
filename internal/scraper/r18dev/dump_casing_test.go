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

// Use an imported Store rather than a lookup stub: Search reaches LookupMovie
// before MatchByDisplayID, so both selection paths must preserve raw identity.
func scraperCasingStore(t *testing.T, rows []string, reverse bool) *r18devdump.Store {
	t.Helper()
	rows = append([]string(nil), rows...)
	if reverse {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	path := filepath.Join(t.TempDir(), "dump.db")
	fixture := "COPY public.derived_video (content_id, dvd_id, title_en) FROM stdin;\n" + strings.Join(rows, "\n") + "\n\\.\n"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(fixture), path, r18devdump.ImportOptions{
		SourceURL: "https://example.invalid/synthetic-casing.sql", RequireAllRows: true,
	})
	require.NoError(t, err)
	store, err := r18devdump.ValidateSidecar(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

func TestDumpCasing_SearchExactRawIdentityWinsOverDVDNormalization(t *testing.T) {
	for _, shape := range []struct{ name, upper, lower, primaryDVD, otherDVD, primaryID, otherID string }{
		{"plain", "ABC001", "abc001", "ABC-001", "OTHER-002", "ABC-001", "OTHER-002"},
		{"marker", "ABC001H", "abc001h", "ABC-001-HD", "OTHER-002-HD", "ABC-001H", ""},
	} {
		for _, reverseOwners := range []bool{false, true} {
			for _, reverseInsert := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/owners_%t/insertion_%t", shape.name, reverseOwners, reverseInsert), func(t *testing.T) {
					upperDVD, lowerDVD := shape.otherDVD, shape.primaryDVD
					upperID, lowerID := shape.otherID, shape.primaryID
					if reverseOwners {
						upperDVD, lowerDVD = lowerDVD, upperDVD
						upperID, lowerID = lowerID, upperID
					}
					store := scraperCasingStore(t, []string{
						shape.upper + "\t" + upperDVD + "\tUpper source product",
						shape.lower + "\t" + lowerDVD + "\tLower source product",
					}, reverseInsert)
					s, transport := newScraperWithBlockedHTTP(t, store)
					for _, tc := range []struct{ raw, dvd, title string }{
						{shape.upper, upperID, "Upper source product"},
						{shape.lower, lowerID, "Lower source product"},
					} {
						result, err := s.Search(context.Background(), tc.raw)
						require.NoError(t, err, tc.raw)
						require.NotNil(t, result)
						assert.Equal(t, tc.raw, result.ContentID)
						assert.Equal(t, tc.dvd, result.ID)
						assert.Equal(t, tc.title, result.Title)
						assert.Contains(t, result.SourceURL, "combined="+tc.raw+"/json")
					}
					assert.Zero(t, transport.count(), "both exact source rows must resolve locally, not via HTTP or a folded sibling")
				})
			}
		}
	}
}

func TestDumpCasing_SearchExactNullDVDNeverBorrowsSiblingMetadata(t *testing.T) {
	for _, nullRaw := range []string{"ABC001", "abc001"} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse_%t", nullRaw, reverse), func(t *testing.T) {
				otherRaw := "abc001"
				if nullRaw == otherRaw {
					otherRaw = "ABC001"
				}
				store := scraperCasingStore(t, []string{
					nullRaw + "\t\\N\tUnverified source row",
					otherRaw + "\tABC-001\tConflicting sibling metadata",
				}, reverse)
				s, transport := newScraperWithBlockedHTTP(t, store)
				result, err := s.Search(context.Background(), nullRaw)
				require.Error(t, err, "an exact NULL-DVD row needs HTTP verification, which this transport blocks")
				assert.Nil(t, result, "the conflicting DVD-normalized sibling must never supply metadata")
				assert.Positive(t, transport.count(), "the NULL-DVD row must fall back rather than borrow the sibling's local movie")
			})
		}
	}
}

func TestDumpCasing_UniqueCandidatePreservesSourceCaseOnWire(t *testing.T) {
	const raw = "LuLu00441"
	store := scraperCasingStore(t, []string{raw + "\t\\N\tUnverified dump title"}, false)
	s, _ := newScraperWithBlockedHTTP(t, store)
	result, candidates := s.searchFromDump(context.Background(), "LULU-441")
	assert.Nil(t, result)
	require.Equal(t, []models.DumpMatch{{ContentID: raw}}, candidates, "ASCII case aliases must pass the canonical-prefix gate without rewriting the source key")

	var paths []string
	transport := &byURLTransport{responder: func(_ int, path string) (int, string) {
		paths = append(paths, path)
		if strings.Contains(path, "combined="+raw+"/json") {
			return 200, fmt.Sprintf(`{"content_id":%q,"dvd_id":null,"title_en":"Verified response title"}`, raw)
		}
		return 404, ""
	}}
	s.client.SetTransport(transport)
	result, err := s.Search(context.Background(), "LULU-441")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, raw, result.ContentID)
	assert.Equal(t, "Verified response title", result.Title, "candidate metadata must come from the response, never the NULL-DVD dump row")
	assert.Contains(t, result.SourceURL, "combined="+raw+"/json")
	assert.Equal(t, []string{"/videos/vod/movies/detail/-/combined=" + raw + "/json"}, paths, "one raw-case candidate request, with no lowercase resolver probes")
}

func TestDumpCasing_MixedCanonicalPrefixRetainsOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse_%t", reverse), func(t *testing.T) {
			store := scraperCasingStore(t, []string{
				"118ABf00030\t\\N\tFirst dump row",
				"118aBF030\t\\N\tSecond dump row",
			}, reverse)
			s, _ := newScraperWithBlockedHTTP(t, store)
			result, candidates := s.searchFromDump(context.Background(), "ABF-030")
			assert.Nil(t, result)
			require.Equal(t, []models.DumpMatch{{ContentID: "118ABf00030"}, {ContentID: "118aBF030"}}, candidates)

			var paths []string
			s.client.SetTransport(&byURLTransport{responder: func(_ int, path string) (int, string) {
				paths = append(paths, path)
				if strings.Contains(path, "combined=118aBF030/json") {
					return 200, `{"content_id":"118aBF030","dvd_id":"ABF-030","title_en":"Verified second candidate"}`
				}
				return 404, ""
			}})
			result, err := s.Search(context.Background(), "ABF-030")
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, "118aBF030", result.ContentID)
			assert.Equal(t, "Verified second candidate", result.Title)
			assert.Equal(t, []string{
				"/videos/vod/movies/detail/-/combined=118ABf00030/json",
				"/videos/vod/movies/detail/-/combined=118aBF030/json",
			}, paths, "raw-case canonical order must survive insertion order and first-candidate fallthrough")
		})
	}
}

func TestDumpCasing_ParityGateRejectsGapsReorderingAndCollisions(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		rows        []string
	}{
		{"gap", "ABF-030", []string{"118ABF00030\t\\N\tFirst", "436AbF00030\t\\N\tSkipped canonical row"}},
		{"noncanonical_only", "ABF-030", []string{"436AbF00030\t\\N\tCompilation"}},
		{"reordered_exact", "LuLu441", []string{"LuLu00441\t\\N\tDigital", "LuLu441\t\\N\tMono"}},
		{"ambiguous_first", "LULU-441", []string{"LuLu00441\t\\N\tOne raw identity", "LULU00441\t\\N\tOther raw identity", "LuLu441\t\\N\tLater candidate"}},
		{"all_ambiguous", "LULU-441", []string{"LuLu00441\t\\N\tOne raw identity", "LULU00441\t\\N\tOther raw identity"}},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse_%t", tc.name, reverse), func(t *testing.T) {
				store := scraperCasingStore(t, tc.rows, reverse)
				s, transport := newScraperWithBlockedHTTP(t, store)
				result, candidates := s.searchFromDump(context.Background(), tc.query)
				assert.Nil(t, result)
				assert.Empty(t, candidates, "ASCII folding cannot turn a sparse, reordered, or ambiguous match set into a trusted canonical prefix")
				assert.Zero(t, transport.count(), "testing the local gate must not contact a provider")
			})
		}
	}
}

func TestDumpCasing_ParityFoldIsASCIIOnly(t *testing.T) {
	// Unicode EqualFold would equate Kelvin-sign K and long S with ASCII k/s;
	// neither belongs in the SQLite NOCASE alias namespace used by the Store.
	for _, raw := range []string{"Kabc00001", "118ſan00457"} {
		t.Run(raw, func(t *testing.T) {
			store := scraperCasingStore(t, []string{raw + "\t\\N\tUnicode source identity"}, false)
			matches, err := store.MatchByDisplayID(context.Background(), raw)
			require.NoError(t, err)
			require.Equal(t, []models.DumpMatch{{ContentID: raw}}, matches, "the raw Unicode identity exists; only its proposed ASCII alias is untrusted")
			all := r18devdump.ContentIDCandidates(raw)
			require.NotEmpty(t, all)
			require.NotEqual(t, raw, all[0])
			require.True(t, strings.EqualFold(raw, all[0]), "fixture must distinguish Unicode EqualFold from SQLite ASCII NOCASE")
			s, transport := newScraperWithBlockedHTTP(t, store)
			result, candidates := s.searchFromDump(context.Background(), raw)
			assert.Nil(t, result)
			assert.Empty(t, candidates, "a non-ASCII identity must not be promoted through an ASCII-only parity alias")
			assert.Zero(t, transport.count())
		})
	}
}

func TestDumpCasing_SearchPinsRawTIdentityAgainstNormalizedT28Sibling(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse_%t", reverse), func(t *testing.T) {
			store := scraperCasingStore(t, []string{
				"t2812h\tT-2812-HD\tPinned T-series source",
				"9t2800012h\tT28-12-HD\tDifferent T28-series source",
			}, reverse)
			s, transport := newScraperWithBlockedHTTP(t, store)
			result, err := s.Search(context.Background(), "t2812h")
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, "t2812h", result.ContentID)
			assert.Empty(t, result.ID, "the ambiguous compact T/T28 spelling must not manufacture a verified display identity")
			assert.Equal(t, "Pinned T-series source", result.Title)
			assert.Zero(t, transport.count())
		})
	}
}

func TestDumpCasing_SearchUppercaseRentalSuffixPreservesBaseIdentity(t *testing.T) {
	for _, tc := range []struct{ query, raw, other, dvd, wantID string }{
		{"AbC001R", "AbC001", "abc001", "OTHER-002", "OTHER-002"},
		{"AbC001HR", "AbC001H", "abc001h", "OTHER-002-HD", ""},
		{"H_003AbC001HDR", "H_003AbC001HD", "h_003abc001hd", "OTHER-002-HD", ""},
	} {
		t.Run(tc.query, func(t *testing.T) {
			store := scraperCasingStore(t, []string{
				tc.raw + "\t" + tc.dvd + "\tPreserved source identity",
				tc.other + "\tABC-001\tLowercase sibling",
			}, false)
			s, transport := newScraperWithBlockedHTTP(t, store)
			result, err := s.Search(context.Background(), tc.query)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tc.raw, result.ContentID)
			assert.Equal(t, tc.wantID, result.ID)
			assert.Equal(t, "Preserved source identity", result.Title)
			assert.Zero(t, transport.count())
		})
	}
}

func TestDumpCasing_CandidateResponseMustMatchExactStoredIdentity(t *testing.T) {
	for _, responseID := range []string{"lulu00441", "LULU00441"} {
		t.Run(responseID, func(t *testing.T) {
			store := scraperCasingStore(t, []string{
				"LuLu00441\t\\N\tFirst source identity",
				"LuLu441\t\\N\tSecond source identity",
			}, false)
			s, _ := newScraperWithBlockedHTTP(t, store)
			var paths []string
			s.client.SetTransport(&byURLTransport{responder: func(_ int, path string) (int, string) {
				paths = append(paths, path)
				switch {
				case strings.Contains(path, "combined=LuLu00441/json"):
					return 200, fmt.Sprintf(`{"content_id":%q,"dvd_id":"LULU-441","title_en":"Different raw source identity"}`, responseID)
				case strings.Contains(path, "combined=LuLu441/json"):
					return 200, `{"content_id":"LuLu441","dvd_id":"LULU-441","title_en":"Exact verified candidate"}`
				default:
					return 404, ""
				}
			}})
			result, err := s.Search(context.Background(), "LULU-441")
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, "LuLu441", result.ContentID, "a folded response alias is not the raw source identity requested from the dump")
			assert.Equal(t, "Exact verified candidate", result.Title)
			assert.Equal(t, []string{
				"/videos/vod/movies/detail/-/combined=LuLu00441/json",
				"/videos/vod/movies/detail/-/combined=LuLu441/json",
			}, paths, "a case-mismatched response must fall through to the next trusted candidate")
		})
	}
}

func TestDumpCasing_ExactRawMarkerStillSanitizesPublishedIdentity(t *testing.T) {
	for _, tc := range []struct{ dvd, wantID string }{
		{"RCT-156-HD", "RCT-156H"},
		{"RCT-157-HD", ""},
	} {
		t.Run(tc.dvd, func(t *testing.T) {
			const raw = "1RcT00156h"
			store := scraperCasingStore(t, []string{raw + "\t" + tc.dvd + "\tRaw source metadata"}, false)
			s, transport := newScraperWithBlockedHTTP(t, store)
			result, err := s.Search(context.Background(), raw)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, raw, result.ContentID)
			assert.Equal(t, tc.wantID, result.ID, "exact raw ownership preserves metadata but must not publish an unverified remaster display ID")
			assert.Equal(t, "Raw source metadata", result.Title)
			assert.Contains(t, result.SourceURL, "combined="+raw+"/json")
			assert.Zero(t, transport.count())
		})
	}
}

// Exact identity is required only for the dump shortcut. After rejecting that
// response, the ordinary HTTP resolver may independently verify a lowercase
// identity; its result must contain HTTP metadata, never the rejected dump row.
func TestDumpCasing_RejectedRawCandidateAllowsIndependentHTTPFallback(t *testing.T) {
	for _, tc := range []struct {
		query, dvdProbe string
		lowerFetches    int
	}{
		{"LULU-441", "lulu441", 1},
		{"LuLu00441", "lulu00441", 2},
	} {
		t.Run(tc.query, func(t *testing.T) {
			store := scraperCasingStore(t, []string{"LuLu00441\t\\N\tUnverified dump-only title"}, false)
			s, _ := newScraperWithBlockedHTTP(t, store)
			var paths []string
			s.client.SetTransport(&byURLTransport{responder: func(_ int, path string) (int, string) {
				paths = append(paths, path)
				switch path {
				case "/videos/vod/movies/detail/-/combined=LuLu00441/json":
					return 200, `{"content_id":"lulu00441","dvd_id":"LULU-441","title_en":"Rejected case-mismatched candidate response"}`
				case "/videos/vod/movies/detail/-/dvd_id=" + tc.dvdProbe + "/json":
					return 200, `{"content_id":"lulu00441","dvd_id":"LULU-441"}`
				case "/videos/vod/movies/detail/-/combined=lulu00441/json":
					return 200, `{"content_id":"lulu00441","dvd_id":"LULU-441","title_en":"Independent lowercase HTTP result","runtime_mins":147}`
				default:
					return 404, ""
				}
			}})
			result, err := s.Search(context.Background(), tc.query)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, "lulu00441", result.ContentID, "the generic resolver independently selected the lowercase HTTP identity")
			assert.Equal(t, "LULU-441", result.ID, "the verified DVD ID comes from HTTP, not the NULL dump field")
			assert.Equal(t, "Independent lowercase HTTP result", result.Title)
			assert.Equal(t, 147, result.Runtime)
			assert.Equal(t, baseURL+"/videos/vod/movies/detail/-/combined=lulu00441/json", result.SourceURL)
			wantPaths := []string{
				"/videos/vod/movies/detail/-/combined=LuLu00441/json",
				"/videos/vod/movies/detail/-/dvd_id=" + tc.dvdProbe + "/json",
			}
			for i := 0; i < tc.lowerFetches; i++ {
				wantPaths = append(wantPaths, "/videos/vod/movies/detail/-/combined=lulu00441/json")
			}
			assert.Equal(t, wantPaths, paths, "the raw-case candidate must be rejected before the generic resolver independently verifies and fetches the lowercase response")
		})
	}
}
