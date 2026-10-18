package r18devdump

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	dumpstore "github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/require"
)

func buildMarkerSearchDump(t *testing.T, path, cid, dvd string) {
	t.Helper()
	input := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" + cid + "\t" + dvd + "\n\\.\n"
	_, err := dumpstore.Import(context.Background(), strings.NewReader(input), path, dumpstore.ImportOptions{})
	require.NoError(t, err)
}

// Only the ordinary search handler is invoked; no import/reload/delete route.
func TestSearchMarkerIdentityBoundary(t *testing.T) {
	for _, tc := range []struct {
		query, cid, dvd string
		status          int
	}{
		{"T-28123H", "9t28123h", "T28-123-HD", http.StatusNotFound},
		{"T-28123-HD", "55t28123h", "T28-123-HD", http.StatusNotFound},
		{"T28-123H", "t28123h", "T-28123-HD", http.StatusNotFound},
		{"T28-123-HD", "9t2800123h", "T-28123-HD", http.StatusNotFound},
		{"T28\u00a0123\u00a0HD", "9t28123h", "T28-123-HD", http.StatusOK},
		{"T\t28123\tHD", "55t28123h", "T-28123-HD", http.StatusOK},
	} {
		t.Run(tc.query, func(t *testing.T) {
			h, path := newTestHandler(t)
			buildMarkerSearchDump(t, path, tc.cid, tc.dvd)
			w, res := searchJSON(t, h, url.QueryEscape(tc.query))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status == http.StatusOK {
				require.Equal(t, "mapped", res["state"])
				require.Equal(t, tc.cid, res["content_id"])
			}
		})
	}
	t.Run("null candidates remain explicitly unverified", func(t *testing.T) {
		h, path := newTestHandler(t)
		buildMarkerSearchDump(t, path, "55t28123zh", `\N`)
		for _, query := range []string{"T-28123-Z-HD", "55t28123zh"} {
			w, res := searchJSON(t, h, url.QueryEscape(query))
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, "no_dvd_id", res["state"])
			require.Nil(t, res["content_id"])
			require.Nil(t, res["dvd_id"])
			matches := res["matches"].([]any)
			require.Equal(t, "55t28123zh", matches[0].(map[string]any)["content_id"])
		}
	})
}
