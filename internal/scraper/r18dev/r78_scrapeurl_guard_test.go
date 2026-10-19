package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ScrapeURL routes marker-bearing URLs through the same remaster identity
// guard as Search: a response carrying a conflicting display id must not be
// published (1ipx00535zh answered with dvd_id IPX-535-HD would otherwise
// publish IPX-535H and lose the Z catalog variant), while a consistent row
// still publishes normally.
func TestScrapeURL_RemasterGuard(t *testing.T) {
	serve := func(dvdID string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"content_id": "1ipx00535zh",
				"dvd_id": "` + dvdID + `",
				"title_en": "Remaster row",
				"release_date": "2024-01-01",
				"runtime_mins": 120,
				"actresses": [],
				"categories": []
			}`))
		}))
	}

	t.Run("conflicting display id stays unpublished", func(t *testing.T) {
		server := serve("IPX-535-HD")
		defer server.Close()
		s := newR18TestScraper(server, true, "en")
		res, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/combined=1ipx00535zh/json")
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.NotEqual(t, "IPX-535-HD", res.ID, "conflicting display identity must not publish")
		assert.NotEqual(t, "IPX-535H", res.ID, "folded conflicting identity must not publish")
	})

	t.Run("conflicting display URL id is rejected", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"content_id": "1rct00157h",
				"dvd_id": "RCT-157-HD",
				"title_en": "Stale remaster row",
				"actresses": [],
				"categories": []
			}`))
		}))
		defer server.Close()
		s := newR18TestScraper(server, true, "en")
		res, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/id=RCT-156-HD/json")
		assert.Error(t, err)
		assert.Nil(t, res)
	})

	t.Run("consistent display id publishes", func(t *testing.T) {
		server := serve("IPX-535ZH")
		defer server.Close()
		s := newR18TestScraper(server, true, "en")
		res, err := s.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/combined=1ipx00535zh/json")
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, "IPX-535ZH", res.ID)
	})
}
