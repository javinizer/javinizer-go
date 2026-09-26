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

// An unpadded raw marker query (1rct156h) is equivalent to the server's
// padded cid (1rct00156h): the raw identity gates must accept the padded
// form instead of 404-ing.
func TestSearchUnpaddedRawMarkerCIDResolves(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id=rct-156-hd"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156h", "dvd_id": "RCT-156-HD"}`))
			return
		case strings.Contains(path, "combined=1rct00156h"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156h", "dvd_id": "RCT-156-HD", "title_en": "Remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "1rct156h")
	require.NoError(t, err, "the unpadded raw query must resolve the padded server cid")
	require.NotNil(t, result)
	assert.Equal(t, "1rct00156h", result.ContentID)
	assert.Equal(t, "RCT-156H", result.ID)
}

// The raw identity gate stays literal for genuinely different products.
func TestRawRemasterCIDEqualRejectsForeignCIDs(t *testing.T) {
	assert.True(t, rawRemasterCIDEqual("1rct00156h", "1rct156h"))
	assert.True(t, rawRemasterCIDEqual("1rct156h", "1rct00156h"))
	assert.False(t, rawRemasterCIDEqual("1rct00999h", "1rct156h"), "numbers stay distinct")
	assert.False(t, rawRemasterCIDEqual("2rct00156h", "1rct156h"), "catalog prefixes stay distinct")
	assert.False(t, rawRemasterCIDEqual("1rct00156hd", "1rct156h"), "marker spellings stay distinct")
	assert.True(t, rawRemasterCIDEqual("h_003abc00123hd", "h_003abc123hd"))
}

// Dump parity: a dump holding only the padded server row stays trusted for
// an unpadded raw query; a foreign row still falls back to HTTP.
func TestUnpaddedRawRemasterDumpParity(t *testing.T) {
	s := &scraper{dumpLookup: &stubDumpLookup{matches: []models.DumpMatch{{ContentID: "1rct00156h"}}}}
	result, matches := s.searchFromDump(context.Background(), "1rct156h")
	assert.Nil(t, result)
	require.Len(t, matches, 1)
	assert.Equal(t, "1rct00156h", matches[0].ContentID)

	foreign := &scraper{dumpLookup: &stubDumpLookup{matches: []models.DumpMatch{{ContentID: "1rct00999h"}}}}
	result, matches = foreign.searchFromDump(context.Background(), "1rct156h")
	assert.Nil(t, result)
	assert.Nil(t, matches, "foreign dump rows must fall back to the HTTP resolver")

	padded := &scraper{dumpLookup: &stubDumpLookup{matches: []models.DumpMatch{{ContentID: "1rct00156h"}}}}
	result, matches = padded.searchFromDump(context.Background(), "1rct00156h")
	assert.Nil(t, result)
	require.Len(t, matches, 1, "padded raw queries keep exact-prefix trust")
}

// Control: unpadded base cids (no marker) keep resolving through candidate
// expansion.
func TestUnpaddedBaseCIDStillResolves(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "combined=118abp00346") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "118abp00346", "dvd_id": "ABP-346", "title_en": "Base"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "118abp346")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "118abp00346", result.ContentID)
}

// Control: display-marker queries keep resolving unchanged.
func TestDisplayMarkerQueryStillResolves(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id=rct-156-hd"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156h", "dvd_id": "RCT-156-HD"}`))
			return
		case strings.Contains(path, "combined=1rct00156h"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156h", "dvd_id": "RCT-156-HD", "title_en": "Remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "RCT-156-HD")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "1rct00156h", result.ContentID)
}
