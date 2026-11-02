package dmm_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

// This integration contract uses the real browser through the public scraper
// constructor. The proxy is a closed fixture server: it has no upstream client,
// does not implement CONNECT, and cannot forward requests to live DMM services.
func TestNativeBrowserScrapeURLCookieAndRenderingContract(t *testing.T) {
	if os.Getenv("JAVINIZER_TEST_NATIVE_DMM_BROWSER") != "1" {
		t.Skip("dedicated native-browser coverage lane only")
	}
	chrome := os.Getenv("CHROME_BIN")
	require.NotEmpty(t, chrome, "explicit browser admission requires CHROME_BIN")
	resolved, err := exec.LookPath(chrome)
	require.NoError(t, err, "explicit browser admission must not silently skip")
	version, err := exec.Command(resolved, "--version").CombinedOutput()
	require.NoError(t, err, "real browser executable must start: %s", version)
	t.Logf("native browser: %s", strings.TrimSpace(string(version)))

	type observedRequest struct {
		path, query, age, locale, userAgent string
	}
	var mu sync.Mutex
	var observed []observedRequest
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never tunnel TLS or forward unexpected browser background requests.
		if r.Method != http.MethodGet || r.URL.Scheme != "http" || r.URL.Host != "video.dmm.co.jp" || r.Host != "video.dmm.co.jp" {
			http.Error(w, "fixture proxy: upstream access forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/av/content/" || (r.URL.RawQuery != "id=ipx00535" && r.URL.RawQuery != "id=ipx00536") {
			http.Error(w, "fixture proxy: unknown resource", http.StatusNotFound)
			return
		}
		cookieValue := func(name string) string {
			cookie, err := r.Cookie(name)
			if err != nil {
				return ""
			}
			return cookie.Value
		}
		mu.Lock()
		observed = append(observed, observedRequest{r.URL.Path, r.URL.RawQuery, cookieValue("age_check_done"), cookieValue("cklg"), r.UserAgent()})
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Query().Get("id") == "ipx00536" {
			_, _ = fmt.Fprint(w, `<!doctype html><html><head><title>Fixture unavailable</title></head><body>No movie metadata</body></html>`)
			return
		}
		_, _ = fmt.Fprint(w, `<!doctype html><html><head>
<link rel="icon" href="data:,">
</head><body><h1>not rendered</h1><script>
document.querySelector('h1').textContent = 'Native browser rendered fixture';
</script></body></html>`)
	}))
	defer proxy.Close()

	registry := scraperutil.NewScraperRegistry()
	dmm.Register(registry)
	constructor, ok := registry.GetScraperConstructor("dmm")
	require.True(t, ok)
	scraper, err := constructor(scraperutil.ScraperDeps{
		// Both public switches are required; global enablement alone keeps HTTP mode.
		Settings: models.ScraperSettings{Enabled: true, UseBrowser: true, UserAgent: "fixture-ordinary-http-client"},
		Browser:  models.BrowserConfig{Enabled: true, Timeout: 30},
		GlobalProxy: &models.ProxyConfig{
			Enabled: true, DefaultProfile: "fixture",
			Profiles: map[string]models.ProxyProfile{"fixture": {URL: proxy.URL}},
		},
		TimeoutSeconds: 30,
		ScrapeActress:  false,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, scraper.Close()) }()
	handler, ok := scraper.(models.URLHandler)
	require.True(t, ok, "DMM must expose the supported direct-URL contract")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	const sourceURL = "http://video.dmm.co.jp/av/content/?id=ipx00535"
	result, err := handler.ScrapeURL(ctx, sourceURL)
	require.NoError(t, err, "fixture HTTP navigation must work without bypassing TLS/browser security")
	require.NotNil(t, result)
	require.Equal(t, "Native browser rendered fixture", result.Title)
	require.Equal(t, "IPX-535", result.ID)
	require.Equal(t, "ipx00535", result.ContentID)
	require.Equal(t, sourceURL, result.SourceURL)

	result, err = handler.ScrapeURL(ctx, "http://video.dmm.co.jp/av/content/?id=ipx00536")
	require.Nil(t, result, "rendering a page without movie metadata must not invent a movie")
	typed, ok := models.AsScraperError(err)
	require.True(t, ok, "expected typed not-found error, got %v", err)
	require.Equal(t, models.ScraperErrorKindNotFound, typed.Kind)

	mu.Lock()
	requests := append([]observedRequest(nil), observed...)
	mu.Unlock()
	for _, query := range []string{"id=ipx00535", "id=ipx00536"} {
		found := false
		for _, request := range requests {
			if request.path == "/av/content/" && request.query == query {
				found = true
				require.Equal(t, "1", request.age)
				require.Equal(t, "ja", request.locale)
				require.NotEqual(t, "fixture-ordinary-http-client", request.userAgent, "fixture must be reached by browser navigation, not the ordinary HTTP client")
				require.Contains(t, request.userAgent, "Chrome/")
			}
		}
		require.True(t, found, "real browser must reach owned proxy fixture for %s", query)
	}
}
