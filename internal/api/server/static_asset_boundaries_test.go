package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStaticWebRoutesServeAvailableAssets(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "_app"), 0700))
	fixtures := map[string]string{"favicon.ico": "icon bytes", "javinizer.png": "png bytes", "_app/client.js": "console.log('offline fixture');"}
	for name, contents := range fixtures {
		require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(contents), 0600))
	}
	dist := os.DirFS(root)
	router := gin.New()
	registerStaticWebRoutes(router, webUIAssets{distFS: dist, staticFS: http.FS(dist), uiAvailable: true})
	for name, contents := range fixtures {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+name, nil))
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, contents, rec.Body.String())
		})
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_app/missing.js", nil))
	require.Equal(t, http.StatusNotFound, rec.Code, "missing assets must not return a successful page or other file")
}

func TestStaticWebRoutesDoNotInventMissingOptionalAssets(t *testing.T) {
	dist := os.DirFS(t.TempDir())
	router := gin.New()
	registerStaticWebRoutes(router, webUIAssets{distFS: dist, staticFS: http.FS(dist), uiAvailable: true})
	for _, path := range []string{"/favicon.ico", "/javinizer.png"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNotFound, rec.Code)
	}
}

func TestStaticWebRoutesUnavailableAssetsRegisterNothing(t *testing.T) {
	router := gin.New()
	// This is loadWebUIAssets' zero-value result when no UI bundle is available.
	registerStaticWebRoutes(router, webUIAssets{})
	require.Empty(t, router.Routes())
	for _, path := range []string{"/favicon.ico", "/javinizer.png", "/_app/client.js"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNotFound, rec.Code)
	}
}
