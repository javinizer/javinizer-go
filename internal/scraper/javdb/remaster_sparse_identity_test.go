package javdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchSparsePublishedIdentityCannotBeReplacedByRetryFallback(t *testing.T) {
	for _, tc := range []struct {
		name, query, selected, published string
		reject                           bool
	}{
		{"base release", "RCT-156H", "RCT-156H", "RCT-156", true},
		{"neighbor", "RCT-156H", "RCT-156H", "RCT-157H", true},
		{"foreign series", "RCT-156H", "RCT-156H", "IPX-156H", true},
		{"changed marker", "RCT-156H", "RCT-156H", "RCT-156AI", true},
		{"AI base", "dv00899ai", "DV-818AI", "DV-818", true},
		{"AI neighbor", "dv00899ai", "DV-818AI", "DV-819AI", true},
		{"matching sparse", "RCT-156H", "RCT-156H", "RCT-156H", false},
		{"no ID sparse", "RCT-156H", "RCT-156H", "", false},
		{"AI matching sparse", "dv00899ai", "DV-818AI", "DV-818AI", false},
		{"AI no ID sparse", "dv00899ai", "DV-818AI", "", false},
	} {
		for _, source := range []string{"title", "panel"} {
			t.Run(tc.name+"/"+source, func(t *testing.T) {
				var hits atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasPrefix(r.URL.Path, "/search") {
						_, _ = w.Write([]byte(remasterSearchPage(tc.selected)))
						return
					}
					if hits.Add(1) == 1 {
						html := `<html><body><h2 class="title is-4"><strong>` + tc.published + `</strong></h2></body></html>`
						if source == "panel" {
							html = `<html><body><div class="movie-panel-info"><div class="panel-block"><strong>番号:</strong><span class="value">` + tc.published + `</span></div></div></body></html>`
						}
						_, _ = w.Write([]byte(html))
						return
					}
					_, _ = w.Write([]byte(remasterDetailPage("")))
				}))
				defer server.Close()
				s := &scraper{client: resty.New(), enabled: true, baseURL: server.URL, rateLimiter: ratelimit.NewLimiter(0), settings: models.ScraperSettings{Enabled: true}}
				result, err := s.Search(context.Background(), tc.query)
				if tc.reject {
					require.Error(t, err)
					assert.Nil(t, result)
					assert.Equal(t, int32(1), hits.Load(), "conflicting primary identity must not retry")
				} else {
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, tc.selected, result.ID)
					assert.Equal(t, "Some Title", result.Title)
					assert.Equal(t, int32(2), hits.Load())
				}
			})
		}
	}
}
