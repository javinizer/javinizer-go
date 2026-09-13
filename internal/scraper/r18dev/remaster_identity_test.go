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
	for _, id := range []string{"RCT-156H", "RCT-156-HD", "DV-818AI", "1rct00156hd"} {
		t.Run(id, func(t *testing.T) {
			all := r18devdump.ContentIDCandidatesWithMarker(id)
			require.NotEmpty(t, all)
			cid := all[0]
			dump := &stubDumpLookup{matches: []models.DumpMatch{{ContentID: cid}}}
			tr := &candidateAPITransport{body: `{"content_id":"` + cid + `","dvd_id":null,"title_en":"Remaster"}`}
			s := newCandidateScraper(dump, tr)
			result, err := s.Search(context.Background(), id)
			require.NoError(t, err)
			assert.Equal(t, cid, result.ContentID)
			assert.Equal(t, 1, tr.count())
		})
	}
}
