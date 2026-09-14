package r18dev

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTwoDigitRemasterSearch(t *testing.T) {
	for _, marker := range []string{"h", "ai"} {
		for _, available := range []bool{false, true} {
			t.Run(marker+map[bool]string{false: "missing", true: "present"}[available], func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if available && strings.Contains(r.URL.Path, "combined=1abc00012"+marker) {
						_, _ = w.Write([]byte(`{"content_id":"1abc00012` + marker + `","dvd_id":"ABC-12` + strings.ToUpper(marker) + `","title_en":"Remaster"}`))
						return
					}
					_, _ = w.Write([]byte(`{"content_id":"1abc00012","dvd_id":"ABC-12","title_en":"Original"}`))
				}))
				defer server.Close()
				for _, q := range []string{"ABC-12" + strings.ToUpper(marker), "ABC.12." + map[string]string{"h": "HD", "ai": "AI"}[marker]} {
					s := newR18TestScraper(server, true, "en")
					result, err := s.Search(context.Background(), q)
					if !available {
						require.Error(t, err)
						assert.Nil(t, result)
						continue
					}
					require.NoError(t, err)
					assert.Equal(t, "1abc00012"+marker, result.ContentID)
					assert.Equal(t, "ABC-12"+strings.ToUpper(marker), result.ID)
				}
			})
		}
	}
}
