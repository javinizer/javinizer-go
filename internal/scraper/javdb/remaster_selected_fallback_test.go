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

func TestSearchRawAISelectedFallbackWithoutPublishedID(t *testing.T) {
	for _, retry := range []bool{false, true} {
		for _, tc := range []struct {
			name, detail string
			reject       bool
		}{
			{"missing published ID", remasterDetailPage(""), false},
			{"matching published ID", remasterDetailPage("DV-818AI"), false},
			{"neighbor published ID", remasterDetailPage("DV-819AI"), true},
			{"changed marker", remasterDetailPage("DV-818H"), true},
			{"changed series", remasterDetailPage("IPX-818AI"), true},
			{"empty detail stays sparse", "<html><body></body></html>", true},
		} {
			t.Run(tc.name+map[bool]string{false: "/primary", true: "/retry"}[retry], func(t *testing.T) {
				var hits atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasPrefix(r.URL.Path, "/search") {
						_, _ = w.Write([]byte(remasterSearchPage("DV-818AI")))
						return
					}
					if r.URL.Path != "/v/dv818ai" {
						http.NotFound(w, r)
						return
					}
					if hits.Add(1) == 1 && retry {
						_, _ = w.Write([]byte("<html><body></body></html>"))
						return
					}
					_, _ = w.Write([]byte(tc.detail))
				}))
				defer server.Close()
				s := &scraper{client: resty.New(), enabled: true, baseURL: server.URL, rateLimiter: ratelimit.NewLimiter(0), settings: models.ScraperSettings{Enabled: true}}
				result, err := s.Search(context.Background(), "dv00899ai")
				if tc.reject {
					require.Error(t, err)
					assert.Nil(t, result)
				} else {
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, "DV-818AI", result.ID)
					assert.Equal(t, "DV-818AI", result.ContentID)
					assert.Equal(t, "Some Title", result.Title)
				}
				expected := int32(1)
				if retry || tc.name == "empty detail stays sparse" {
					expected = 2
				}
				assert.Equal(t, expected, hits.Load(), "sparse pages must retry without counting a fallback title as metadata")
			})
		}
	}
}
