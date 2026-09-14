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

func TestLongSeriesRemasterSearch(t *testing.T) {
	for _, marker := range []string{"h", "ai"} {
		for _, available := range []bool{false, true} {
			t.Run(marker+map[bool]string{false: "missing", true: "present"}[available], func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if available && strings.Contains(r.URL.Path, "combined=5125abeauty00123"+marker) {
						_, _ = w.Write([]byte(`{"content_id":"5125abeauty00123` + marker + `","dvd_id":"ABEAUTY-123` + strings.ToUpper(marker) + `","title_en":"Remaster"}`))
						return
					}
					_, _ = w.Write([]byte(`{"content_id":"5125abeauty00123","dvd_id":"ABEAUTY-123","title_en":"Original"}`))
				}))
				defer server.Close()
				for _, q := range []string{"ABEAUTY-123" + strings.ToUpper(marker), "ABEAUTY.123." + map[string]string{"h": "HD", "ai": "AI"}[marker]} {
					s := newR18TestScraper(server, true, "en")
					result, err := s.Search(context.Background(), q)
					if !available {
						require.Error(t, err)
						assert.Nil(t, result)
						continue
					}
					require.NoError(t, err)
					assert.Equal(t, "5125abeauty00123"+marker, result.ContentID)
					assert.Equal(t, "ABEAUTY-123"+strings.ToUpper(marker), result.ID)
				}
			})
		}
	}
}
