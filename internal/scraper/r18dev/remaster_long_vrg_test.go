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

func TestLongPrefixVRGRemasterSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "combined=5750360vrg00123h") {
			_, _ = w.Write([]byte(`{"content_id":"5750360vrg00123h","dvd_id":"VRG-123-HD","title_en":"Remaster"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	for _, q := range []string{"VRG-123-HD", "VRG-123H"} {
		s := newR18TestScraper(server, true, "en")
		result, err := s.Search(context.Background(), q)
		require.NoError(t, err)
		assert.Equal(t, "5750360vrg00123h", result.ContentID)
		assert.Equal(t, "VRG-123H", result.ID)
	}
	assert.False(t, markerVariationAccept([]byte(`{"content_id":"5750360vrg00123","dvd_id":"VRG-123-HD"}`), "VRG-123H", "h", "vrg"))
}
