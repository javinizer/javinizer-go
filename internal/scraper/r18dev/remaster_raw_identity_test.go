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
)

func TestRawRemasterRequiresLiteralIdentity(t *testing.T) {
	for _, tc := range []struct {
		query, cid string
		want       bool
	}{
		{"1rct00156h", "1rct00999h", false},
		{"1rct00156h", "2rct00156h", false},
		{"1rct00156h", "1rct00156h", true},
		{"1RCT00156H", "1rct00156h", true},
		{"h_003abc00123hd", "h_003abc00999hd", false},
		{"h_003abc00123hd", "n_003abc00123hd", false},
		{"h_003abc00123hd", "h_003abc00123h", false},
		{"h_003abc00123hd", "h_003abc00123hd", true},
		{"n_600abc00123h", "n_600abc00123h", true},
	} {
		t.Run(tc.query+"_"+tc.cid, func(t *testing.T) {
			marker, series := classifyRemaster(tc.query)
			require.NotEmpty(t, marker)
			require.True(t, isRawRemasterContentIDQuery(tc.query))
			assert.Equal(t, tc.want, cidMatchesRemasterQuery(tc.cid, tc.query, marker, series))
			body := []byte(`{"content_id":"` + tc.cid + `","dvd_id":null}`)
			assert.Equal(t, tc.want, markerVariationAccept(body, tc.query, marker, series))
			_, err := guardRemasterResult(tc.query, &models.ScraperResult{ContentID: tc.cid})
			assert.Equal(t, !tc.want, err != nil)
		})
	}
}

func TestSearchRawRemasterRejectsWrongNumber(t *testing.T) {
	for _, finalOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "lookup_and_probe", true: "final_response"}[finalOnly], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				cid := "1rct00999h"
				if finalOnly && strings.Contains(r.URL.Path, "dvd_id=") {
					cid = "1rct00156h"
				}
				_, _ = w.Write([]byte(`{"content_id":"` + cid + `","dvd_id":null,"title_en":"Remaster"}`))
			}))
			defer server.Close()
			s := newR18TestScraper(server, true, "en")
			result, err := s.Search(context.Background(), "1rct00156h")
			require.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

func TestSearchRemasterUnderscoreCatalogPrefixes(t *testing.T) {
	for _, cid := range []string{"n_600abc00123h", "h_003abc00123h"} {
		for _, query := range []string{"ABC-123H", cid} {
			t.Run(query+"_"+cid, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"content_id":"` + cid + `","dvd_id":null,"title_en":"Prefixed remaster"}`))
				}))
				defer server.Close()
				s := newR18TestScraper(server, true, "en")
				result, err := s.Search(context.Background(), query)
				require.NoError(t, err)
				assert.Equal(t, cid, result.ContentID)
				if query == "ABC-123H" {
					assert.Equal(t, query, result.ID)
				}
			})
		}
	}
	assert.False(t, cidMatchesMarker("h_003abc00123h", "h", "rct"))
	assert.False(t, cidMatchesMarker("n_600abc00123", "h", "abc"))
	assert.False(t, isRawRemasterContentIDQuery("ABC_123_HD"))
}
