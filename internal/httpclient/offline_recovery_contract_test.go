package httpclient

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recoveryRoundTripper func(*http.Request) (*http.Response, error)

func (f recoveryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func malformedRecoveryProxy() *models.ProxyConfig {
	return &models.ProxyConfig{
		Enabled:        true,
		DefaultProfile: "malformed",
		Profiles: map[string]models.ProxyProfile{
			"malformed": {URL: "http://proxy.invalid/%zz"},
		},
	}
}

func TestOfflineRecoveryBuildersRejectMalformedResolvedProxy(t *testing.T) {
	for _, mode := range []string{"client", "proxy", "flaresolverr"} {
		t.Run(mode, func(t *testing.T) {
			builder := FromScraperSettings(nil, malformedRecoveryProxy(), models.FlareSolverrConfig{})
			var err error
			switch mode {
			case "client":
				client, buildErr := builder.BuildClient()
				assert.Nil(t, client, "a malformed resolved proxy must not return a usable client")
				err = buildErr
			case "proxy":
				client, profile, buildErr := builder.BuildWithProxy()
				assert.Nil(t, client)
				assert.Nil(t, profile)
				err = buildErr
			case "flaresolverr":
				client, fs, buildErr := builder.BuildWithFlareSolverr()
				assert.Nil(t, client)
				assert.Nil(t, fs)
				err = buildErr
			}
			require.ErrorContains(t, err, "failed to create HTTP client: invalid proxy URL")
			var parseErr *url.Error
			require.ErrorAs(t, err, &parseErr, "preserve the underlying proxy parse error")
			assert.Equal(t, "parse", parseErr.Op)
		})
	}
}

func TestOfflineRecoveryInitUsesExplicitNoProxyFallback(t *testing.T) {
	// The global proxy must be enabled; a scraper-only override is intentionally
	// ignored by the resolver's global circuit breaker and cannot test fallback.
	settings := &models.ScraperSettings{Timeout: 7, RetryCount: 2}
	result := InitScraperClient(settings, malformedRecoveryProxy(), models.FlareSolverrConfig{})
	require.NotNil(t, result)
	require.NotNil(t, result.Client)
	assert.Equal(t, 7*time.Second, result.Client.GetClient().Timeout)
	assert.Equal(t, 2, result.Client.RetryCount)
	transport, ok := result.Client.GetClient().Transport.(*http.Transport)
	require.True(t, ok)
	assert.Nil(t, transport.Proxy, "fallback must not retain the broken proxy or consult proxy environment variables")
	assert.True(t, result.ProxyEnabled, "resolved configuration remains available for diagnostics")
	require.NotNil(t, result.ProxyProfile)
	assert.Equal(t, "http://proxy.invalid/%zz", result.ProxyProfile.URL)
	assert.Nil(t, result.FlareSolverr)
}

func TestOfflineRecoveryBuilderPreservesPresetCookies(t *testing.T) {
	client, err := FromScraperSettings(&models.ScraperSettings{
		Cookies: map[string]string{"session": "fixture-token"},
	}, nil, models.FlareSolverrConfig{}, WithHeaders(DMMHeaders())).BuildClient()
	require.NoError(t, err)
	wantCookie := DMMHeaders()["Cookie"] + "; session=fixture-token"
	assert.Equal(t, wantCookie, client.Header.Get("Cookie"))
	calls := 0
	client.SetTransport(recoveryRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		assert.Equal(t, "https://metadata.invalid/item", req.URL.String())
		assert.Equal(t, wantCookie, req.Header.Get("Cookie"), "configured cookies must extend, not replace, the age/language preset")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader("fixture")),
			Request:    req,
		}, nil
	}))
	response, err := client.R().Get("https://metadata.invalid/item")
	require.NoError(t, err)
	assert.Equal(t, "fixture", response.String())
	assert.Equal(t, 1, calls)
}

func TestOfflineRecoveryFlareSolverrDiscardsStaleSessionAfterFailedRecreation(t *testing.T) {
	for _, destroyFailure := range []string{"http", "envelope", "transport"} {
		t.Run(destroyFailure, func(t *testing.T) {
			fs, err := newFlareSolverr(&models.FlareSolverrConfig{
				URL: "http://solver.invalid/v1", Timeout: 3, MaxRetries: 0, SessionTTL: 120,
			})
			require.NoError(t, err)
			const firstURL = "https://metadata.invalid/first"
			const recoveredURL = "https://metadata.invalid/recovered"
			const nextURL = "https://metadata.invalid/next"
			type exchange struct {
				request flareSolverrRequest
				status  int
				body    string
				err     error
			}
			steps := []exchange{
				{request: flareSolverrRequest{Cmd: "sessions.create"}, body: `{"status":"ok","session":"stale-session"}`},
				{request: flareSolverrRequest{Cmd: "request.get", URL: firstURL, MaxTimeout: 3000, Session: "stale-session", SessionTTLMinutes: 2}, body: `{"status":"ok","solution":{"response":"first","cookies":[{"name":"auth","value":"stale-cookie"}]}}`},
				{request: flareSolverrRequest{Cmd: "request.get", URL: recoveredURL, MaxTimeout: 3000, Session: "stale-session", SessionTTLMinutes: 2}, body: `{"status":"error","message":"session expired"}`},
				{request: flareSolverrRequest{Cmd: "sessions.destroy", Session: "stale-session"}, body: `{"status":"error","message":"session unavailable"}`},
				{request: flareSolverrRequest{Cmd: "sessions.create"}, status: http.StatusServiceUnavailable, body: `{"status":"error","message":"session creation unavailable"}`},
				{request: flareSolverrRequest{Cmd: "request.get", URL: recoveredURL, MaxTimeout: 3000}, body: `{"status":"ok","solution":{"response":"one-off recovery","cookies":[{"name":"auth","value":"one-off-cookie"}]}}`},
				{request: flareSolverrRequest{Cmd: "sessions.create"}, body: `{"status":"ok","session":"fresh-session"}`},
				{request: flareSolverrRequest{Cmd: "request.get", URL: nextURL, MaxTimeout: 3000, Session: "fresh-session", SessionTTLMinutes: 2}, body: `{"status":"ok","solution":{"response":"fresh session"}}`},
			}
			switch destroyFailure {
			case "http":
				steps[3].status = http.StatusServiceUnavailable
			case "transport":
				steps[3].err = errors.New("fixture transport failure")
			}
			calls := 0
			fs.client.SetTransport(recoveryRoundTripper(func(req *http.Request) (*http.Response, error) {
				require.Less(t, calls, len(steps), "unexpected extra protocol request")
				step := steps[calls]
				calls++
				assert.Equal(t, http.MethodPost, req.Method)
				assert.Equal(t, "http://solver.invalid/v1", req.URL.String())
				var payload map[string]json.RawMessage
				require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
				raw, err := json.Marshal(payload)
				require.NoError(t, err)
				var actual flareSolverrRequest
				require.NoError(t, json.Unmarshal(raw, &actual))
				assert.Equal(t, step.request, actual, "protocol request %d", calls)
				if step.request.Session == "" {
					assert.NotContains(t, payload, "session", "one-off/create requests must omit the session field")
					assert.NotContains(t, payload, "session_ttl_minutes")
				}
				if step.err != nil {
					return nil, step.err
				}
				status := step.status
				if status == 0 {
					status = http.StatusOK
				}
				return &http.Response{
					StatusCode: status,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(step.body)),
					Request:    req,
				}, nil
			}))

			html, cookies, err := fs.ResolveURL(firstURL)
			require.NoError(t, err)
			assert.Equal(t, "first", html)
			require.Len(t, cookies, 1)
			assert.Equal(t, "stale-cookie", cookies[0].Value)
			require.Equal(t, 2, calls)

			html, cookies, err = fs.ResolveURL(recoveredURL)
			require.NoError(t, err)
			assert.Equal(t, "one-off recovery", html)
			require.Len(t, cookies, 1)
			assert.Equal(t, "auth", cookies[0].Name)
			assert.Equal(t, "one-off-cookie", cookies[0].Value)
			require.Equal(t, 6, calls, "one recreation attempt must precede the session-free fallback")
			assert.Empty(t, fs.persistentSessionID)
			_, cached := fs.sessions.Load("stale-session")
			assert.False(t, cached, "remote destroy failure must not preserve stale local cookies")

			html, cookies, err = fs.ResolveURL(nextURL)
			require.NoError(t, err)
			assert.Equal(t, "fresh session", html)
			assert.Empty(t, cookies, "neither stale-session nor one-off cookies may leak into a new session")
			assert.Equal(t, "fresh-session", fs.persistentSessionID)
			cachedSession, cached := fs.sessions.Load("fresh-session")
			require.True(t, cached)
			session, ok := cachedSession.(*flareSolverrSession)
			require.True(t, ok)
			assert.Equal(t, []string{nextURL}, session.URLs)
			assert.Empty(t, session.Cookies)
			assert.Equal(t, len(steps), calls)
		})
	}
}
