package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A filesystem may refuse a subtree even while its other assets remain readable.
// Keep this failure at the fs.SubFS boundary, rather than depending on host
// permissions or mutating the embedded production bundle.
type deniedAppSubtreeFS struct {
	fs.FS
	subCalls []string
}

func (f *deniedAppSubtreeFS) Sub(dir string) (fs.FS, error) {
	f.subCalls = append(f.subCalls, dir)
	return nil, &fs.PathError{Op: "sub", Path: dir, Err: fs.ErrPermission}
}

func TestStaticWebRoutesSubtreeFailurePreservesOtherAssetsAndSPA(t *testing.T) {
	dist := &deniedAppSubtreeFS{FS: fstest.MapFS{
		"favicon.ico":   &fstest.MapFile{Data: []byte("icon bytes")},
		"javinizer.png": &fstest.MapFile{Data: []byte("png bytes")},
		"robots.txt":    &fstest.MapFile{Data: []byte("User-agent: *\nDisallow: /api/\n")},
	}}
	assets := webUIAssets{
		distFS: dist, staticFS: http.FS(dist),
		indexHTML: []byte("<!doctype html><title>Offline fixture</title>"), uiAvailable: true,
	}
	router := gin.New()
	registerStaticWebRoutes(router, assets)
	registerNoRouteHandler(router, assets, nil)
	require.Equal(t, []string{"_app"}, dist.subCalls)
	for _, route := range router.Routes() {
		require.NotContains(t, route.Path, "/_app", "a refused subtree must not register static routes")
	}

	for _, tc := range []struct {
		path, accept, body string
		status             int
	}{
		{"/favicon.ico", "", "icon bytes", http.StatusOK},
		{"/javinizer.png", "", "png bytes", http.StatusOK},
		{"/robots.txt", "", "User-agent: *\nDisallow: /api/\n", http.StatusOK},
		{"/settings", "text/html", string(assets.indexHTML), http.StatusOK},
		{"/_app/client.js", "application/javascript", "", http.StatusNotFound},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Accept", tc.accept)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
			if tc.status == http.StatusOK {
				require.Equal(t, tc.body, rec.Body.String())
			} else {
				require.JSONEq(t, `{"error":"Not Found","message":"The requested resource does not exist","path":"/_app/client.js","method":"GET"}`, rec.Body.String())
				require.NotContains(t, rec.Body.String(), "permission denied")
			}
		})
	}
}
