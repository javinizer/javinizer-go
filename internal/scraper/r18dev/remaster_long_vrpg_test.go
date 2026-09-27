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

func TestLongPrefixVRPGRemasterSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "combined=5755360vrpg00123h") {
			_, _ = w.Write([]byte(`{"content_id":"5755360vrpg00123h","dvd_id":"VRPG-123-HD","title_en":"Remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	for _, q := range []string{"VRPG-123-HD", "VRPG-123H"} {
		s := newR18TestScraper(server, true, "en")
		result, err := s.Search(context.Background(), q)
		require.NoError(t, err)
		assert.Equal(t, "5755360vrpg00123h", result.ContentID)
		assert.Equal(t, "VRPG-123H", result.ID)
	}
	assert.False(t, markerVariationAccept([]byte(`{"content_id":"5755360vrpg00123","dvd_id":"VRPG-123-HD"}`), "VRPG-123H", "h", "vrpg"))
}
