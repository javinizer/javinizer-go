package workflow_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestFactory_NamedDownloadProxyPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, wantProxy string
		override        bool
	}{
		{name: "enabled_download_override", override: true, wantProxy: "download"},
		{name: "disabled_override_uses_global", override: false, wantProxy: "global"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type observation struct {
				server, method, requestURI, host, proxyAuth, marker string
				close                                               bool
			}
			var mu sync.Mutex
			var observed []observation
			newEndpoint := func(name string) *httptest.Server {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					observed = append(observed, observation{
						server: name, method: r.Method, requestURI: r.RequestURI, host: r.Host,
						proxyAuth: r.Header.Get("Proxy-Authorization"), marker: r.Header.Get("X-Download-Fixture"), close: r.Close,
					})
					mu.Unlock()
					// These owned proxies answer directly; no request is forwarded.
					w.Header().Set("Content-Type", "text/plain")
					_, _ = io.WriteString(w, name+"-response")
				}))
				server.Config.SetKeepAlivesEnabled(false)
				t.Cleanup(server.Close)
				return server
			}
			origin := newEndpoint("origin")
			globalProxy := newEndpoint("global")
			downloadProxy := newEndpoint("download")

			cfg := config.DefaultConfig(nil, nil)
			cfg.Database.DSN = filepath.Join(t.TempDir(), "factory-proxy.db")
			cfg.System.TempDir = t.TempDir()
			cfg.Scrapers.Proxy = models.ProxyConfig{
				Enabled: true, DefaultProfile: "global",
				Profiles: map[string]models.ProxyProfile{
					"global":   {URL: globalProxy.URL, Username: "global-fixture-user", Password: "global-fixture-password"},
					"download": {URL: downloadProxy.URL, Username: "download-fixture-user", Password: "download-fixture-password"},
				},
			}
			cfg.Output.Download.DownloadProxy = models.ProxyConfig{Enabled: tc.override, Profile: "download"}
			_, err := config.Prepare(cfg)
			require.NoError(t, err)
			original := cfg.Clone()

			db, err := database.New(database.ConfigFromAppConfig(cfg))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			factory, err := workflow.NewFactoryConfigFromRepos(cfg, scraperutil.NewScraperRegistry(), db.Repositories())
			require.NoError(t, err, "real repositories must produce a fully constructed factory")
			require.NotNil(t, factory.Scraper)
			require.NotNil(t, factory.HTTPClient)
			require.Equal(t, original, cfg, "factory construction must not rewrite configured routing")

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			destination := origin.URL + "/media/poster.jpg?fixture=named-proxy"
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, destination, nil)
			require.NoError(t, err)
			req.Header.Set("X-Download-Fixture", "preserved-marker")
			req.Close = true // The client must not retain idle connections past fixture teardown.
			resp, err := factory.HTTPClient.Do(req)
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })
			body, readErr := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			require.NoError(t, readErr)
			require.NoError(t, closeErr)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, tc.wantProxy+"-response", string(body))
			require.True(t, resp.Request.Context() == ctx, "routing must preserve the caller's request context")
			require.Empty(t, req.Header.Get("Proxy-Authorization"), "proxy credentials must not mutate the caller's headers")
			require.Equal(t, original, cfg, "routing must not rewrite either named profile")

			profile := original.Scrapers.Proxy.Profiles[tc.wantProxy]
			wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(profile.Username+":"+profile.Password))
			mu.Lock()
			got := append([]observation(nil), observed...)
			mu.Unlock()
			require.Equal(t, []observation{{
				server: tc.wantProxy, method: http.MethodGet, requestURI: destination, host: req.URL.Host,
				proxyAuth: wantAuth, marker: "preserved-marker", close: true,
			}}, got, "only the selected proxy may receive the request; origin and the other proxy must remain untouched")
		})
	}
}
