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

func TestTSeriesSeparatorIdentity(t *testing.T) {
	for _, q := range []string{"T-28123-HD", "T-28123H"} {
		marker, series := classifyRemaster(q)
		assert.Equal(t, "h", marker)
		assert.Equal(t, "t", series)
	}
	assert.Equal(t, []string{"t-28123-hd"}, remasterDisplaySpellings("T-28123-HD"))
	assert.Equal(t, []string{"t28-123-hd"}, remasterDisplaySpellings("T28-123-HD"))
	assert.Equal(t, "T-28123H", canonicalRemasterDisplayID("T-28123-HD"), "canonicalization must keep the separator-pinned series identity")
	assert.Equal(t, "T28-123H", canonicalRemasterDisplayID("T28-123-HD"))
	marker, series := classifyRemaster("12345-HD")
	assert.Equal(t, "", marker)
	assert.Equal(t, "", series)
}

func TestT28RemasterSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "combined=9t28123h") {
			_, _ = w.Write([]byte(`{"content_id":"9t28123h","dvd_id":"T28-123-HD","title_en":"Remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	for _, q := range []string{"T28-123-HD", "T28-123H"} {
		s := newR18TestScraper(server, true, "en")
		result, err := s.Search(context.Background(), q)
		require.NoError(t, err)
		assert.Equal(t, "9t28123h", result.ContentID)
		assert.Equal(t, "T28-123H", result.ID)
	}
	assert.False(t, markerVariationAccept([]byte(`{"content_id":"9t28123","dvd_id":"T28-123-HD"}`), "T28-123H", "h", "t28"))
}
