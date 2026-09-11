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

func TestRemasterRejectsConflictingDisplay(t *testing.T) {
	for _, dvd := range []string{"RCT-999-HD", "RCT-156", "DV-156H"} {
		t.Run(dvd, func(t *testing.T) {
			body := `{"content_id":"1rct00999h","dvd_id":"` + dvd + `","title_en":"Wrong release"}`
			assert.False(t, markerVariationAccept([]byte(body), "RCT-156H", "h", "rct"))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "combined=") {
					_, _ = w.Write([]byte(body))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()
			s := newR18TestScraper(server, true, "en")
			result, err := s.Search(context.Background(), "RCT-156H")
			require.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

func TestRemasterFinalFetchRejectsChangedDisplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "dvd_id=") {
			_, _ = w.Write([]byte(`{"content_id":"1rct00156h","dvd_id":"RCT-156-HD"}`))
			return
		}
		_, _ = w.Write([]byte(`{"content_id":"1rct00156h","dvd_id":"RCT-999-HD","title_en":"Wrong release"}`))
	}))
	defer server.Close()
	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "RCT-156H")
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestRemasterDumpRejectsConflictingDisplay(t *testing.T) {
	dump := &stubDumpLookup{lookupMovieResult: &models.DumpMovie{ContentID: "1rct00999h", DVDID: "RCT-999-HD"}}
	s, _ := newScraperWithBlockedHTTP(t, dump)
	result, candidates := s.searchFromDump(context.Background(), "RCT-156H")
	assert.Nil(t, result)
	assert.Empty(t, candidates)
}
