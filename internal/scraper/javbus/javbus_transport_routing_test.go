package javbus

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJavbusRoundTripperExactRouting(t *testing.T) {
	const searchURL = "https://www.javbus.com/search/IPX-456&type=0&parent=uc"
	const detailURL = "https://www.javbus.com/ja/IPX-456"
	for _, tc := range []struct {
		name       string
		requestURL string
		responses  map[string]string
		statuses   map[string]int
		wantStatus int
		wantBody   string
	}{
		{
			name: "exact search wins over overlapping detail fragment", requestURL: searchURL,
			responses:  map[string]string{searchURL: "search", "/IPX-456": "wrong detail"},
			wantStatus: http.StatusOK, wantBody: "search",
		},
		{
			name: "exact detail preserves its configured status", requestURL: detailURL,
			responses:  map[string]string{searchURL: "search", detailURL: "detail error"},
			statuses:   map[string]int{detailURL: http.StatusInternalServerError},
			wantStatus: http.StatusInternalServerError, wantBody: "detail error",
		},
		{
			name: "colliding search and detail fragments never route", requestURL: searchURL,
			responses:  map[string]string{"/search/": "wrong search", "/IPX-456": "wrong detail"},
			wantStatus: http.StatusNotFound, wantBody: "not found",
		},
		{
			name: "wrong host does not reuse matching path", requestURL: "https://www.javbus.org/ja/IPX-456",
			responses:  map[string]string{detailURL: "detail"},
			wantStatus: http.StatusNotFound, wantBody: "not found",
		},
		{
			name: "wrong language path does not reuse suffix", requestURL: "https://www.javbus.com/IPX-456",
			responses:  map[string]string{detailURL: "detail"},
			wantStatus: http.StatusNotFound, wantBody: "not found",
		},
		{
			name: "unexpected query does not reuse URL prefix", requestURL: detailURL + "?unexpected=1",
			responses:  map[string]string{detailURL: "detail"},
			wantStatus: http.StatusNotFound, wantBody: "not found",
		},
		{
			name: "unconfigured URL remains not found", requestURL: "https://www.javbus.com/unknown",
			responses:  map[string]string{searchURL: "search", detailURL: "detail"},
			wantStatus: http.StatusNotFound, wantBody: "not found",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := &javbusRoundTripper{responses: tc.responses, statuses: tc.statuses}
			req, err := http.NewRequest(http.MethodGet, tc.requestURL, nil)
			require.NoError(t, err)
			resp, err := rt.RoundTrip(req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			defer func() { require.NoError(t, resp.Body.Close()) }()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, resp.StatusCode)
			require.Equal(t, tc.wantBody, string(body))
			require.Same(t, req, resp.Request)
			require.Equal(t, []string{tc.requestURL}, rt.lastReqs)
		})
	}
}
