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

func TestIsRawRemasterContentIDQuery(t *testing.T) {
	assert.True(t, isRawRemasterContentIDQuery("1rct00156h"))
	assert.True(t, isRawRemasterContentIDQuery("lulu00441ai"))
	assert.False(t, isRawRemasterContentIDQuery("RCT-156H"))
	assert.False(t, isRawRemasterContentIDQuery("RCT 156 HD"))
	assert.False(t, isRawRemasterContentIDQuery("oreco183h"))
}

func TestCanonicalRemasterDisplayIDR18(t *testing.T) {
	assert.Equal(t, "DV-818AI", canonicalRemasterDisplayID("DV-818AI"))
	assert.Equal(t, "RCT-156H", canonicalRemasterDisplayID("RCT-156-HD"))
	assert.Equal(t, "PLAIN", canonicalRemasterDisplayID("plain"))
}

func TestRemasterHelpers(t *testing.T) {
	assert.False(t, cidCarriesMarker("dv00899ai", ""))
	assert.False(t, cidCarriesMarker("", "h"))
	assert.Nil(t, remasterDisplaySpellings("RCT-156"))

	assert.False(t, markerVariationAccept([]byte("not json"), "RCT-156H", "h", "rct"))
	assert.False(t, markerVariationAccept([]byte(`{"content_id":"1rct00156","dvd_id":"RCT-156-HD"}`), "RCT-156H", "h", "rct"))
	assert.True(t, markerVariationAccept([]byte(`{"content_id":"dv00899ai","dvd_id":null}`), "DV-818AI", "ai", "dv"))
	assert.False(t, markerVariationAccept([]byte(`{"content_id":"1rct00156","dvd_id":"RCT-156"}`), "RCT-156H", "h", "rct"))
	assert.False(t, markerVariationAccept([]byte(`{"content_id":"dv00899h","dvd_id":null}`), "RCT-156H", "h", "rct"), "stale foreign marker cid must be rejected by series")

	assert.True(t, cidMatchesMarker("1rct00156h", "h", "rct"))
	assert.False(t, cidMatchesMarker("1dv00899h", "h", "rct"), "marker alone is not enough: series must match")

	guarded, gerr := guardRemasterResult("RCT-156H", &models.ScraperResult{ContentID: "dv00899h", ID: "DV-899H"})
	assert.Error(t, gerr, "foreign marker identity must fail the result guard")
	assert.Nil(t, guarded)

	guarded, gerr = guardRemasterResult("ABC-123H", &models.ScraperResult{ContentID: "118abc00123h", ID: "ABC-123H"})
	assert.NoError(t, gerr, "three-digit catalog prefixes must pass the guard")
	require.NotNil(t, guarded)

	guarded, gerr = guardRemasterResult("dv00899ai", &models.ScraperResult{ContentID: "dv00899ai", ID: "DV-899AI"})
	assert.NoError(t, gerr)
	require.NotNil(t, guarded)
	assert.Equal(t, "DV-899AI", guarded.ID, "a server-provided display equal to the cid echo is preserved; provenance is decided in resolveIDs, not by value")

	guarded, gerr = guardRemasterResult("dv00899ai", &models.ScraperResult{ContentID: "dv00899ai", ID: "DV-818-AI"})
	assert.NoError(t, gerr)
	require.NotNil(t, guarded)
	assert.Equal(t, "DV-818AI", guarded.ID, "server-provided display ID is canonicalized")

	guarded, gerr = guardRemasterResult("1ipx00535zh", &models.ScraperResult{ContentID: "1ipx00535zh", ID: "IPX-535-HD"})
	assert.NoError(t, gerr)
	require.NotNil(t, guarded)
	assert.Equal(t, "", guarded.ID, "an explicit dvd_id conflicting with the cid E/Z identity stays unset")

	guarded, gerr = guardRemasterResult("dv00899ai", &models.ScraperResult{ContentID: "dv00899ai", ID: "DV-899H"})
	assert.NoError(t, gerr)
	assert.Equal(t, "", guarded.ID, "a mismatched marker on the explicit dvd_id stays unset")

	guarded, gerr = guardRemasterResult("dv00899ai", &models.ScraperResult{ContentID: "dv00899ai", ID: "unparseable"})
	assert.NoError(t, gerr)
	assert.Equal(t, "", guarded.ID, "an unparseable explicit display cannot be verified and stays unset")

	guarded, gerr = guardRemasterResult("ABC-123H", &models.ScraperResult{ContentID: "118abc00123", ID: "ABC-123"})

	assert.Equal(t, "", resolveIDs(&r18Response{ContentID: "dv00899ai"}), "null dvd_id with a marker cid leaves the ID unset: cid numbers are slot numbers")
	assert.Equal(t, "", resolveIDs(&r18Response{ContentID: "1abc00123h"}), "marker cids never synthesize an echo")
	assert.Equal(t, "ABW-013", resolveIDs(&r18Response{ContentID: "118abw00013"}), "marker-free cids keep the derived echo")
	assert.Equal(t, "DV-818AI", resolveIDs(&r18Response{ContentID: "dv00899ai", DVDID: "DV-818AI"}), "an explicit dvd_id always wins")
	assert.Error(t, gerr, "wide-prefix base release must still fail a marker query")
	assert.Nil(t, guarded)
}

// Fuzzy path with a marker-carrying content id: the recording gate accepts,
// variation probes all miss, the step-3 fuzzy URL is returned, and the final
// guard passes because the fetched record carries the marker.
func TestRemaster_FuzzyMarkerRecordedAndReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id=rct-156-hd"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content_id": "1rct00156h", "dvd_id": null}`))
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
	result, err := s.Search(context.Background(), "RCT-156H")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "1rct00156h", result.ContentID)
}

// A dump-resolved candidate carrying base identity must be rejected by the
// marker guard and never returned.
func TestRemaster_DumpCandidateGuardRejectsBase(t *testing.T) {
	dump := &stubDumpLookup{
		matches: []models.DumpMatch{{ContentID: "1rct00156", DVDID: "RCT-156"}},
	}
	transport := &candidateAPITransport{body: `{"content_id": "1rct00156", "dvd_id": "RCT-156", "title_en": "Base"}`}
	s := newCandidateScraper(dump, transport)

	result, err := s.Search(context.Background(), "RCT-156H")
	require.Error(t, err)
	assert.Nil(t, result)
}
