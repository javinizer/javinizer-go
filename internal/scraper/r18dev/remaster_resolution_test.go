package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

func TestSearchSkipsMarkerlessVariation(t *testing.T) {
	candidates := r18devdump.ContentIDCandidatesWithMarker("RCT-156H")
	require.Greater(t, len(candidates), 1)
	secondFetched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "combined="+candidates[0]+"/json") {
			_, _ = w.Write([]byte(`{"content_id":"1rct00156","dvd_id":"RCT-156-HD","title_en":"Stale"}`))
			return
		}
		if strings.Contains(r.URL.Path, "combined="+candidates[1]+"/json") {
			secondFetched = true
			_, _ = w.Write([]byte(`{"content_id":"` + candidates[1] + `","dvd_id":"RCT-156-HD","title_en":"Remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	s := newR18TestScraper(server, true, "en")
	result, err := s.Search(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.True(t, secondFetched)
	assert.Equal(t, candidates[1], result.ContentID)
	assert.Equal(t, "Remaster", result.Title)
}

func TestRealDumpRawUnderscoreRemasterSingleFetch(t *testing.T) {
	for _, cid := range []string{"h_003abc00123h", "h_003abc00123hd", "n_600abc00123ai"} {
		t.Run(cid, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dump.db")
			dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" + cid + "\t\\N\n\\.\n"
			_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{})
			require.NoError(t, err)
			store, err := r18devdump.Open(path)
			require.NoError(t, err)
			defer store.Close()
			tr := &candidateAPITransport{body: `{"content_id":"` + cid + `","dvd_id":null,"title_en":"Remaster"}`}
			cfg := createTestSettings(true)
			s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, store)
			s.client.SetRetryCount(0)
			s.client.SetTransport(tr)
			result, err := s.Search(context.Background(), cid)
			require.NoError(t, err)
			assert.Equal(t, cid, result.ContentID)
			assert.Equal(t, 1, tr.count())
		})
	}
}
