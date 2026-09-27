package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

func TestRawRemasterSuffixIdentity(t *testing.T) {
	for _, tc := range []struct {
		query, cid string
		want       bool
	}{
		{"1rct00156hd", "1rct00156h", false},
		{"1rct00156h", "1rct00156hd", false},
		{"1rct00156hd", "1rct00156hd", true},
		{"118abc00123hd", "118abc00123h", false},
		{"118abc00123hd", "118abc00123hd", true},
		{"rct00156hd", "rct00156h", false},
		{"RCT-156-HD", "1rct00156h", true},
		{"RCT-156H", "1rct00156hd", true},
	} {
		t.Run(tc.query+"_"+tc.cid, func(t *testing.T) {
			marker, series := classifyRemaster(tc.query)
			assert.Equal(t, tc.want, cidMatchesRemasterQuery(tc.cid, tc.query, marker, series))
			if isRawRemasterContentIDQuery(tc.query) {
				body := []byte(`{"content_id":"` + tc.cid + `","dvd_id":"RCT-156-HD"}`)
				assert.Equal(t, tc.want, markerVariationAccept(body, tc.query, marker, series))
			}
			result, err := guardRemasterResult(tc.query, &models.ScraperResult{ContentID: tc.cid})
			if tc.want {
				require.NoError(t, err)
				require.NotNil(t, result)
			} else {
				require.Error(t, err)
				assert.Nil(t, result)
			}
		})
	}
}

func TestSearchRawHDRejectsHFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content_id":"1rct00156h","dvd_id":null,"title_en":"Distinct H product"}`))
	}))
	defer server.Close()
	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "1rct00156hd")
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestSearchSkipsMarkerlessDisplayMatch(t *testing.T) {
	for _, dvd := range []string{"RCT-156-HD", "RCT156H"} {
		t.Run(dvd, func(t *testing.T) {
			baseFetched := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "dvd_id=") {
					_, _ = w.Write([]byte(`{"content_id":"1rct00156","dvd_id":"` + dvd + `"}`))
					return
				}
				if strings.Contains(r.URL.Path, "combined=1rct00156/json") {
					baseFetched = true
				}
				_, _ = w.Write([]byte(`{"content_id":"1rct00156h","dvd_id":"RCT-156-HD","title_en":"Correct remaster"}`))
			}))
			defer server.Close()
			s := newR18TestScraper(server, true, "en")
			result, err := s.Search(context.Background(), "RCT-156H")
			require.NoError(t, err)
			assert.Equal(t, "1rct00156h", result.ContentID)
			assert.False(t, baseFetched)
		})
	}
}

func TestDumpRemasterCandidateSingleFetch(t *testing.T) {
	for _, tc := range []struct {
		id     string
		dvdID  string // served dvd_id; "" renders null
		wantID string // expected published display ID
	}{
		{"RCT-156H", "", "RCT-156H"},
		{"RCT-156-HD", "", "RCT-156H"},
		{"1rct00156hd", "", "RCT-156H"},
		// An AI display query cannot verify a null-dvd_id row (cid numbers
		// diverge by design), so the AI case needs a matching dvd_id to
		// resolve through the display comparison.
		{"DV-818AI", "DV-818AI", "DV-818AI"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			all := r18devdump.ContentIDCandidatesWithMarker(tc.id)
			require.NotEmpty(t, all)
			cid := all[0]
			dvdField := "null"
			if tc.dvdID != "" {
				dvdField = `"` + tc.dvdID + `"`
			}
			dump := &stubDumpLookup{matches: []models.DumpMatch{{ContentID: cid}}}
			tr := &candidateAPITransport{body: `{"content_id":"` + cid + `","dvd_id":` + dvdField + `,"title_en":"Remaster"}`}
			s := newCandidateScraper(dump, tr)
			result, err := s.Search(context.Background(), tc.id)
			require.NoError(t, err)
			assert.Equal(t, cid, result.ContentID)
			assert.Equal(t, 1, tr.count())
			assert.Equal(t, tc.wantID, result.ID)
		})
	}
}

// A dump-resolved AI candidate whose fetched row has a null dvd_id must be
// rejected: AI cid numbers are slot numbers that diverge from display
// numbers, so the row carries no evidence tying it to the display query —
// the search falls through and misses instead of publishing unrelated
// metadata with an empty ID.
func TestDumpRemasterAICandidate_NullDVDIDRejected(t *testing.T) {
	all := r18devdump.ContentIDCandidatesWithMarker("DV-818AI")
	require.NotEmpty(t, all)
	cid := all[0]
	dump := &stubDumpLookup{matches: []models.DumpMatch{{ContentID: cid}}}
	tr := &candidateAPITransport{body: `{"content_id":"` + cid + `","dvd_id":null,"title_en":"Remaster"}`}
	s := newCandidateScraper(dump, tr)
	result, err := s.Search(context.Background(), "DV-818AI")
	require.Error(t, err, "a null-dvd_id AI row must not publish for an AI display query")
	assert.Nil(t, result)
	assert.Greater(t, tr.count(), 0, "the candidate must have been fetched and rejected by the guard")
}
