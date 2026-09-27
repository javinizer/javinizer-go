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

func TestSearchDottedRemasterCombinedResolution(t *testing.T) {
	for _, query := range []string{"RCT-156.HD", "RCT.00156.HD", "RCT-00156-HD", "RCT-00156H"} {
		t.Run(query, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "combined=1rct00156h/json") {
					_, _ = w.Write([]byte(`{"content_id":"1rct00156h","dvd_id":"RCT-156-HD","title_en":"Remaster"}`))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()
			s := newR18TestScraper(server, true, "en")
			result, err := s.Search(context.Background(), query)
			require.NoError(t, err)
			assert.Equal(t, "1rct00156h", result.ContentID)
			assert.Equal(t, "Remaster", result.Title)
		})
	}
}
