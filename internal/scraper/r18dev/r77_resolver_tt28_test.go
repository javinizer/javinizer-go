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

// A dvd_id variation whose row is internally inconsistent (content_id
// t28123h with dvd_id T28-123-HD) satisfies both the folded display compare
// and the dash-collapsed normalized compare for a T-28123-HD query, but its
// separator-pinned identity disagrees. Resolution must skip that row and
// continue to the remaining variations until a consistent row wins.
func TestSearch_T28123HD_SkipsConflictingEarlyVariation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "dvd_id=t28123hd"):
			// Normalized/compacted spelling hits the conflicting T28 row.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"content_id": "t28123h",
				"dvd_id": "T28-123-HD",
				"title": "Conflicting T28 row"
			}`))
			return
		case strings.Contains(path, "dvd_id=t-28123-hd"):
			// Display-spelling variation hits the consistent T-series row.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"content_id": "t28123h",
				"dvd_id": "T-28123-HD",
				"title": "Consistent row"
			}`))
			return
		case strings.Contains(path, "combined=t28123h"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"content_id": "t28123h",
				"dvd_id": "T-28123-HD",
				"title_en": "Consistent T series release",
				"release_date": "2024-01-01",
				"runtime_mins": 120,
				"actresses": [],
				"categories": []
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")

	res, err := s.Search(context.Background(), "T-28123-HD")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "t28123h", res.ContentID)
}
