package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The normalized-ID fallback path of Search fetches the combined URL built
// purely from the query spelling; a malformed JSON body there must surface a
// parse error rather than a silent zero-value result.
func TestSearch_NormalizedFallbackBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not-json"))
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	_, err := s.Search(context.Background(), "IPX-999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse R18.dev response")
}
