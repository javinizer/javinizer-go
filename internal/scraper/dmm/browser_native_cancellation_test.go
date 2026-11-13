package dmm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestNativeBrowserCancellationAfterNavigation(t *testing.T) {
	if os.Getenv("JAVINIZER_TEST_NATIVE_DMM_BROWSER") != "1" {
		t.Skip("dedicated native-browser coverage lane only")
	}
	chrome := os.Getenv("CHROME_BIN")
	require.NotEmpty(t, chrome, "explicit browser admission requires CHROME_BIN")
	resolved, err := exec.LookPath(chrome)
	require.NoError(t, err)
	versionCtx, versionCancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer versionCancel()
	version, err := exec.CommandContext(versionCtx, resolved, "--version").CombinedOutput()
	require.NoError(t, err, "real browser executable must start: %s", version)
	t.Logf("native browser: %s", strings.TrimSpace(string(version)))
	for _, public := range []bool{false, true} {
		name := "fetch"
		if public {
			name = "scrape_url"
		}
		t.Run(name, func(t *testing.T) {
			const authority = "video.dmm.co.jp:18080"
			const sourceURL = "http://" + authority + "/av/content/?id=ipx00535"
			type requestEvidence struct{ userAgent, age, locale string }
			admitted := make(chan requestEvidence, 1)
			requestClosed := make(chan struct{})
			release := make(chan struct{})
			var accepted atomic.Bool
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Scheme != "http" || r.URL.Host != authority || r.Host != authority || r.URL.Path != "/av/content/" || r.URL.RawQuery != "id=ipx00535" {
					http.Error(w, "fixture proxy: upstream access forbidden", http.StatusForbidden)
					return
				}
				if !accepted.CompareAndSwap(false, true) {
					http.Error(w, "fixture proxy: duplicate navigation", http.StatusConflict)
					return
				}
				cookie := func(name string) string {
					c, err := r.Cookie(name)
					if err != nil {
						return ""
					}
					return c.Value
				}
				admitted <- requestEvidence{r.UserAgent(), cookie("age_check_done"), cookie("cklg")}
				select {
				case <-r.Context().Done():
					close(requestClosed)
				case <-release:
				}
			}))
			defer proxy.Close()
			profile := &models.ProxyProfile{URL: proxy.URL}
			type outcome struct {
				hasResult bool
				err       error
			}
			invoke := func(ctx context.Context) outcome {
				body, err := fetchWithBrowser(ctx, sourceURL, 30, profile, os.Getenv, afero.NewOsFs())
				return outcome{body != "", err}
			}
			if public {
				registry := scraperutil.NewScraperRegistry()
				Register(registry)
				constructor, ok := registry.GetScraperConstructor("dmm")
				require.True(t, ok)
				scraper, err := constructor(scraperutil.ScraperDeps{
					Settings:       models.ScraperSettings{Enabled: true, UseBrowser: true, UserAgent: "fixture-ordinary-http-client"},
					Browser:        models.BrowserConfig{Enabled: true, Timeout: 30},
					GlobalProxy:    &models.ProxyConfig{Enabled: true, DefaultProfile: "fixture", Profiles: map[string]models.ProxyProfile{"fixture": *profile}},
					TimeoutSeconds: 30,
				})
				require.NoError(t, err)
				defer func() { require.NoError(t, scraper.Close()) }()
				handler, ok := scraper.(models.URLHandler)
				require.True(t, ok)
				require.True(t, handler.CanHandleURL(sourceURL))
				invoke = func(ctx context.Context) outcome {
					result, err := handler.ScrapeURL(ctx, sourceURL)
					return outcome{result != nil, err}
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			result := make(chan outcome, 1)
			finished := make(chan struct{})
			defer func() {
				cancel()
				close(release)
				proxy.CloseClientConnections()
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					t.Error("browser worker did not stop during cleanup")
				}
			}()
			go func() { defer close(finished); result <- invoke(ctx) }()
			select {
			case evidence := <-admitted:
				require.Contains(t, evidence.userAgent, "Chrome/")
				require.NotEqual(t, "fixture-ordinary-http-client", evidence.userAgent)
				require.Equal(t, "1", evidence.age)
				require.Equal(t, "ja", evidence.locale)
			case early := <-result:
				t.Fatalf("browser returned before admitted navigation: result=%t error=%v", early.hasResult, early.err)
			case <-time.After(45 * time.Second):
				t.Fatal("real browser did not reach the closed proxy fixture")
			}
			cancel()
			select {
			case got := <-result:
				require.False(t, got.hasResult, "cancelled navigation must not publish HTML or a movie")
				if public {
					typed, ok := models.AsScraperError(got.err)
					require.True(t, ok, "expected typed scraper error, got %v", got.err)
					require.Equal(t, "DMM", typed.Scraper)
					require.Contains(t, typed.Message, "browser automation failed: context canceled")
				} else {
					require.ErrorIs(t, got.err, context.Canceled)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("browser fetch did not return promptly after cancellation")
			}
			select {
			case <-requestClosed:
			case <-time.After(10 * time.Second):
				t.Fatal("browser cancellation did not close the admitted request context")
			}
			select {
			case <-finished:
			case <-time.After(10 * time.Second):
				t.Fatal("browser worker did not finish after cancellation")
			}
		})
	}
}
