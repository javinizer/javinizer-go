package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaleDumpRemasterFallsBackToHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "combined=1rct00156h/json") {
			_, _ = w.Write([]byte(`{"content_id":"1rct00156h","dvd_id":"RCT-156-HD","title_en":"Live remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	s := newR18TestScraper(server, true, "en")
	s.dumpLookup = &stubDumpLookup{lookupMovieResult: &models.DumpMovie{DVDID: "RCT-156-HD", ContentID: "1rct00156", TitleEn: "Stale"}}
	result, err := s.Search(context.Background(), "RCT-156-HD")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", result.ContentID)
	assert.Equal(t, "Live remaster", result.Title)
}
