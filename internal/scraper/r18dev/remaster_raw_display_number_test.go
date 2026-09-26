package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The raw-CID analog of the display-query number guards (rounds 10/14): an
// H/HD remaster content id keeps the display number (1rct00156h is
// RCT-156H), so a raw query answered with its exact content id but a
// CONFLICTING dvd_id (RCT-157-HD) must not be canonicalized and published
// under the wrong release ID. AI content ids diverge from display numbers by
// design (dv00899ai is DV-818AI) and stay number-free.

// rawDisplayMatchesCID unit coverage: H/HD binds the display number to the
// cid number, padding-normalized with the same TrimLeft semantics as
// cidMatchesRemasterFuzzyQuery, while AI stays number-free.
func TestRawDisplayMatchesCID_NumberBinding(t *testing.T) {
	cases := []struct {
		name     string
		cid      string
		display  string
		expected bool
	}{
		{"h matching display", "1rct00156h", "RCT-156-HD", true},
		{"h matching folded display", "1rct00156h", "RCT-156H", true},
		{"h padded display", "1rct00156h", "RCT-00156-HD", true},
		{"h raw display echo", "1rct00156h", "1rct00156h", true},
		{"h conflicting number rejected", "1rct00156h", "RCT-157-HD", false},
		{"h conflicting unpadded number rejected", "1rct156h", "RCT-157H", false},
		{"h t-series folded number", "t28123h", "T-28123-HD", true},
		{"h t-series conflicting number rejected", "t28123h", "T-999-HD", false},
		{"h ez identity still required", "1ipx00535zh", "IPX-535-HD", false},
		{"h series still required", "1rct00156h", "IPX-156-HD", false},
		{"h marker still required", "1rct00156h", "RCT-156-AI", false},
		{"h unparseable display rejected", "1rct00156h", "unparseable", false},
		// AI content ids diverge from display numbers (dv00899ai is
		// DV-818AI): number-free, series/marker-based acceptance.
		{"ai divergent number accepted", "dv00899ai", "DV-818AI", true},
		{"ai raw display echo accepted", "dv00899ai", "dv00899ai", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, rawDisplayMatchesCID(tc.cid, tc.display))
		})
	}
}

// A raw H/HD query answered with its exact content id but a conflicting
// dvd_id (RCT-157-HD for 1rct00156h) must not be canonicalized and published
// as RCT-157H: the guard leaves the display ID unset so the requested
// release is never sorted under the wrong ID.
func TestRemaster_RawHQuery_ConflictingDisplayNumberStaysUnpublished(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content_id": "1rct00156h", "dvd_id": "RCT-157-HD", "title_en": "Wrong release"}`))
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "1rct00156h")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "1rct00156h", result.ContentID)
	assert.Equal(t, "", result.ID, "a conflicting dvd_id must leave the display ID unset, not publish RCT-157H")
}

// The same raw query with a MATCHING dvd_id publishes under the correct
// canonical display ID (RCT-156H) for both hyphenated and folded spellings.
func TestRemaster_RawHQuery_MatchingDisplayNumberPublishes(t *testing.T) {
	for _, dvd := range []string{"RCT-156-HD", "RCT-156H"} {
		t.Run(dvd, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"content_id": "1rct00156h", "dvd_id": "` + dvd + `", "title_en": "Correct release"}`))
			}))
			defer server.Close()

			s := newR18TestScraper(server, true, "en")
			result, err := s.Search(context.Background(), "1rct00156h")
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, "1rct00156h", result.ContentID)
			assert.Equal(t, "RCT-156H", result.ID)
		})
	}
}

// A raw AI query keeps the number-free acceptance: the page's cid
// (dv00899ai) and its dvd_id (DV-818AI) diverge by design, so the
// server-provided display is still canonicalized and published verbatim.
func TestRemaster_RawAIQuery_NumberDivergenceStillPublishes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content_id": "dv00899ai", "dvd_id": "DV-818AI", "title_en": "AI remaster"}`))
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "dv00899ai")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "dv00899ai", result.ContentID)
	assert.Equal(t, "DV-818AI", result.ID)
}
