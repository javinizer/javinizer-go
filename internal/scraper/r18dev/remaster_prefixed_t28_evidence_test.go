package r18dev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/require"
)

// Only ordinary, read-only identity lookup is exercised against this Store.
func prefixedT28EvidenceStore(t *testing.T, cid, dvd string) *r18devdump.Store {
	t.Helper()
	if dvd == "" {
		dvd = `\N`
	}
	path := filepath.Join(t.TempDir(), "identity.db")
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" + cid + "\t" + dvd + "\n\\.\n"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{})
	require.NoError(t, err)
	store, err := r18devdump.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func prefixedT28Body(cid, dvd string) []byte {
	display := "null"
	if dvd != "" {
		display = fmt.Sprintf("%q", dvd)
	}
	return []byte(fmt.Sprintf(`{"content_id":%q,"dvd_id":%s,"title_en":"Identity fixture"}`, cid, display))
}

func TestPrefixedT28PublishedIdentity(t *testing.T) {
	for _, prefix := range []string{"55", "874", "1038", "66", "h_66", "n_003"} {
		for _, tail := range []string{"123", "00123", "1234"} {
			for _, boundary := range []string{"T", "T28"} {
				number := strings.TrimLeft(tail, "0")
				if boundary == "T" {
					number = "28" + tail
				}
				query := boundary + "-" + number + "-HD"
				for _, mode := range []string{"store", "http"} {
					t.Run(prefix+"/"+tail+"/"+boundary+"/"+mode, func(t *testing.T) {
						cid := prefix + "t28" + tail + "h"
						var requests atomic.Int32
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							requests.Add(1)
							w.Header().Set("Content-Type", "application/json")
							_, _ = w.Write(prefixedT28Body(cid, query))
						}))
						defer server.Close()
						s := newR18TestScraper(server, true, "en")
						if mode == "store" {
							s.dumpLookup = prefixedT28EvidenceStore(t, cid, query)
						}
						res, err := s.Search(context.Background(), query)
						require.NoError(t, err)
						require.Equal(t, cid, res.ContentID)
						require.Equal(t, boundary+"-"+number+"H", res.ID)
						if mode == "store" {
							require.Zero(t, requests.Load(), "published dump identity must be usable without HTTP")
						}
					})
				}
			}
		}
	}
}

func TestPrefixedT28MissingOrConflictingEvidence(t *testing.T) {
	for _, prefix := range []string{"55", "874", "1038", "66", "h_66", "n_003"} {
		for _, query := range []string{"T-28123-HD", "T28-123-HD"} {
			for _, evidence := range []string{"null", "opposite", "neighbor", "base", "marker", "suffix"} {
				for _, mode := range []string{"http", "store_then_http"} {
					t.Run(prefix+"/"+query+"/"+evidence+"/"+mode, func(t *testing.T) {
						cid := prefix + "t28123h"
						dvd := ""
						switch evidence {
						case "opposite":
							dvd = "T-28123-HD"
							if strings.HasPrefix(query, "T-") {
								dvd = "T28-123-HD"
							}
						case "neighbor":
							dvd = strings.Replace(query, "123", "124", 1)
						case "base":
							dvd = strings.TrimSuffix(query, "-HD")
						case "marker":
							dvd = strings.Replace(query, "-HD", "-AI", 1)
						case "suffix":
							dvd = strings.Replace(query, "-HD", "E-HD", 1)
						}
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							w.Header().Set("Content-Type", "application/json")
							_, _ = w.Write(prefixedT28Body(cid, dvd))
						}))
						defer server.Close()
						s := newR18TestScraper(server, true, "en")
						if mode == "store_then_http" {
							s.dumpLookup = prefixedT28EvidenceStore(t, cid, dvd)
						}
						res, err := s.Search(context.Background(), query)
						require.Error(t, err)
						require.Nil(t, res)
					})
				}
			}
		}
	}
}

func TestPrefixedT28RawIdentity(t *testing.T) {
	for _, prefix := range []string{"55", "874", "1038", "66", "h_66", "n_003"} {
		for _, tail := range []string{"123", "00123"} {
			cid := prefix + "t28" + tail + "h"
			for _, evidence := range []string{"null", "T", "T28", "padded_neighbor", "padded_neighbor_with_display"} {
				for _, route := range []string{"search", "store_then_http", "url"} {
					t.Run(cid+"/"+evidence+"/"+route, func(t *testing.T) {
						returnedCID, dvd, wantID := cid, "", ""
						if evidence == "T" {
							dvd = "T-28" + tail + "-HD"
							wantID = "T-28" + tail + "H"
						}
						if evidence == "T28" {
							dvd = "T28-" + strings.TrimLeft(tail, "0") + "-HD"
							wantID = "T28-" + strings.TrimLeft(tail, "0") + "H"
						}
						if strings.HasPrefix(evidence, "padded_neighbor") {
							neighbor := "00123"
							if tail == "00123" {
								neighbor = "123"
							}
							returnedCID = prefix + "t28" + neighbor + "h"
							if evidence == "padded_neighbor_with_display" {
								dvd = "T28-123-HD"
							}
						}
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							w.Header().Set("Content-Type", "application/json")
							_, _ = w.Write(prefixedT28Body(returnedCID, dvd))
						}))
						defer server.Close()
						s := newR18TestScraper(server, true, "en")
						if route == "store_then_http" {
							s.dumpLookup = prefixedT28EvidenceStore(t, returnedCID, dvd)
						}
						if route == "url" {
							res, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/combined="+cid+"/json")
							if strings.HasPrefix(evidence, "padded_neighbor") {
								require.Error(t, err)
								require.Nil(t, res)
								return
							}
							require.NoError(t, err)
							require.Equal(t, cid, res.ContentID)
							require.Equal(t, wantID, res.ID)
						} else {
							res, err := s.Search(context.Background(), cid)
							if strings.HasPrefix(evidence, "padded_neighbor") {
								require.Error(t, err)
								require.Nil(t, res)
								return
							}
							require.NoError(t, err)
							require.Equal(t, cid, res.ContentID)
							require.Equal(t, wantID, res.ID)
						}
					})
				}
			}
		}
	}
}

func TestPrefixedT28MarkerAndSuffixEvidence(t *testing.T) {
	for _, prefix := range []string{"55", "874", "1038", "66", "h_66", "n_003"} {
		for _, suffix := range []string{"h", "hd", "ai", "eh", "zhd", "eai", "zai"} {
			for _, boundary := range []string{"T", "T28"} {
				t.Run(prefix+"/"+suffix+"/"+boundary, func(t *testing.T) {
					cid := prefix + "t28123" + suffix
					number := "123"
					if boundary == "T" {
						number = "28123"
					}
					if strings.HasSuffix(suffix, "ai") {
						number = "818"
					}
					query := boundary + "-" + number + "-" + strings.ToUpper(suffix)
					marker, series := classifyRemaster(query)
					body := []byte(fmt.Sprintf(`{"content_id":%q,"dvd_id":%q}`, cid, query))
					require.True(t, markerVariationAccept(body, query, marker, series))
					require.True(t, rawDisplayMatchesCID(cid, query))
					require.False(t, cidMatchesRemasterFuzzyQuery(cid, query, marker, series))
					require.Empty(t, resolveIDs(&r18Response{ContentID: cid}))
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(body)
					}))
					defer server.Close()
					s := newR18TestScraper(server, true, "en")
					res, err := s.Search(context.Background(), query)
					require.NoError(t, err)
					require.Equal(t, cid, res.ContentID)
					require.Equal(t, canonicalRemasterDisplayID(query), res.ID)
				})
			}
		}
	}
}

func TestPrefixedT28RawEchoDoesNotProveBoundary(t *testing.T) {
	for _, dvd := range []string{"55t28123h", "9t28123h", "h_66t28123h", "unparseable", ""} {
		t.Run(dvd, func(t *testing.T) {
			cid := "55t28123h"
			require.False(t, rawDisplayMatchesCID(cid, dvd))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"content_id":%q,"dvd_id":%q}`, cid, dvd)
			}))
			defer server.Close()
			s := newR18TestScraper(server, true, "en")
			res, err := s.Search(context.Background(), cid)
			require.NoError(t, err)
			require.Equal(t, cid, res.ContentID)
			require.Empty(t, res.ID)
		})
	}
	for _, dvd := range []string{"T28123H", "T2800123H", "T-28123-HD", "T28-123-HD"} {
		require.True(t, rawDisplayMatchesCID("55t28123h", dvd), dvd)
	}
}

func TestPrefixedT28ExistingIdentityControls(t *testing.T) {
	for _, prefix := range []string{"9", "1", "h_003"} {
		t.Run(prefix, func(t *testing.T) {
			cid := prefix + "t2800123h"
			query := prefix + "t28123h"
			_, unresolved := unprovenT28CIDNumber(cid)
			require.False(t, unresolved)
			require.True(t, rawRemasterCIDEqual(cid, query))
			require.Equal(t, "T28-123H", resolveIDs(&r18Response{ContentID: cid}))
			require.True(t, cidMatchesRemasterFuzzyQuery(cid, "T28-123-HD", "h", "t28"))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"content_id":%q,"dvd_id":null}`, cid)
			}))
			defer server.Close()
			s := newR18TestScraper(server, true, "en")
			res, err := s.Search(context.Background(), query)
			require.NoError(t, err)
			require.Equal(t, cid, res.ContentID)
			require.Equal(t, "T28-123H", res.ID)
		})
	}
	require.True(t, rawRemasterCIDEqual("1rct00156h", "1rct156h"))
	require.False(t, rawRemasterCIDEqual("1t2800123h", "55t28123h"))
	require.True(t, rawRemasterCIDEqual("55t28123h", "55T28123H"))
	require.False(t, rawDisplayMatchesCID("55t28123h", "T-28124-HD"))
	require.False(t, rawDisplayMatchesCID("55t28123h", "T28-124-HD"))
	require.False(t, rawDisplayMatchesCID("55t28123h", "T-28123-EHD"))
	require.False(t, rawDisplayMatchesCID("55t28123h", "T-28123-AI"))
}
