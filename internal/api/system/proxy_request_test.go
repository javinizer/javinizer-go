package system

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/require"
)

func TestResolveProxyTestProfile(t *testing.T) {
	saved := models.ProxyProfile{URL: "http://saved:8080", Username: "saved-user", Password: "saved-password"}
	draft := models.ProxyProfile{URL: "http://draft:8080"}
	persisted := models.ProxyConfig{Enabled: true, DefaultProfile: "main", Profiles: map[string]models.ProxyProfile{"main": saved}}
	for _, tc := range []struct {
		name    string
		request models.ProxyConfig
		want    models.ProxyProfile
	}{
		{"saved fallback", models.ProxyConfig{Enabled: true}, saved},
		{"named saved fallback", models.ProxyConfig{Enabled: true, Profile: "main"}, saved},
		{"default saved fallback", models.ProxyConfig{Enabled: true, DefaultProfile: "main"}, saved},
		{"disabled", models.ProxyConfig{}, models.ProxyProfile{}},
		{"empty profiles do not use saved", models.ProxyConfig{Enabled: true, Profiles: map[string]models.ProxyProfile{}}, models.ProxyProfile{}},
		{"missing requested profile", models.ProxyConfig{Enabled: true, Profile: "missing", Profiles: map[string]models.ProxyProfile{"main": draft}}, models.ProxyProfile{}},
		{"non-default draft without stale credentials", models.ProxyConfig{Enabled: true, Profile: "backup", Profiles: map[string]models.ProxyProfile{"backup": draft}}, draft},
		{"disabled draft", models.ProxyConfig{Profile: "main", Profiles: map[string]models.ProxyProfile{"main": draft}}, models.ProxyProfile{}},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, *resolveProxyTestProfile(persisted, tc.request)) })
	}
}

func TestProxy_RedactedRequestCredentials(t *testing.T) {
	t.Cleanup(ssrf.SetLookupIPForTest(func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}))
	for _, tc := range []struct {
		name      string
		requested models.ProxyProfile
		expected  models.ProxyProfile
	}{
		{"both redacted", models.ProxyProfile{Username: models.RedactedValue, Password: models.RedactedValue}, models.ProxyProfile{Username: "saved-user", Password: "saved-password"}},
		{"new username", models.ProxyProfile{Username: "new-user", Password: models.RedactedValue}, models.ProxyProfile{Username: "new-user", Password: "saved-password"}},
		{"new password", models.ProxyProfile{Username: models.RedactedValue, Password: "new-password"}, models.ProxyProfile{Username: "saved-user", Password: "new-password"}},
		{"cleared credentials", models.ProxyProfile{}, models.ProxyProfile{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantAuth := ""
				if tc.expected.Username != "" || tc.expected.Password != "" {
					wantAuth = "Basic " + base64.StdEncoding.EncodeToString([]byte(tc.expected.Username+":"+tc.expected.Password))
				}
				if r.Header.Get("Proxy-Authorization") != wantAuth {
					w.WriteHeader(http.StatusProxyAuthRequired)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer proxy.Close()
			cfg := config.DefaultConfig(nil, nil)
			cfg.Scrapers.Proxy = models.ProxyConfig{Enabled: true, DefaultProfile: "main", Profiles: map[string]models.ProxyProfile{
				"main":   {URL: "http://old-proxy:8080", Username: "saved-user", Password: "saved-password"},
				"backup": {URL: "http://backup:8080", Username: "backup-user", Password: "backup-password"},
			}}
			before, err := json.Marshal(cfg.Scrapers.Proxy)
			require.NoError(t, err)
			tokens := core.NewTokenStore()
			deps := newTestDeps(cfg, func(d *core.APIDeps) { d.TokenStore = tokens })
			rt := testkit.GetTestRuntime(deps)
			router := gin.New()
			router.POST("/proxy/test", testProxy(rt))
			requested := tc.requested
			requested.URL = proxy.URL
			requestProxy := models.ProxyConfig{Enabled: true, Profile: "main", Profiles: map[string]models.ProxyProfile{
				"main":   requested,
				"backup": {URL: "http://backup:8080", Username: models.RedactedValue, Password: models.RedactedValue},
			}}
			body, err := json.Marshal(contracts.ProxyTestRequest{Mode: "direct", TargetURL: "http://example.com/proxy-probe", Proxy: requestProxy})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var response contracts.ProxyTestResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.True(t, response.Success, response.Message)
			expected := tc.expected
			expected.URL = proxy.URL
			requestProxy.Profile = ""
			requestProxy.DefaultProfile = "main"
			requestProxy.Profiles["main"] = expected
			requestProxy.Profiles["backup"] = cfg.Scrapers.Proxy.Profiles["backup"]
			hash, err := core.HashProxyConfig(requestProxy)
			require.NoError(t, err)
			require.True(t, tokens.Validate(response.VerificationToken, "global", hash))
			after, err := json.Marshal(rt.GetAPIConfig().ProxyConfig)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
		})
	}
}

func TestProxy_RequestProfiles(t *testing.T) {
	t.Cleanup(ssrf.SetLookupIPForTest(func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}))
	for _, tc := range []struct {
		name       string
		persisted  models.ProxyConfig
		useDefault bool
	}{
		{name: "first setup"},
		{name: "default profile field", useDefault: true},
		{name: "replace saved profile", persisted: models.ProxyConfig{Enabled: true, DefaultProfile: "main", Profiles: map[string]models.ProxyProfile{"main": {URL: "http://127.0.0.1:1"}}}},
		{name: "enable disabled saved proxy", persisted: models.ProxyConfig{DefaultProfile: "main", Profiles: map[string]models.ProxyProfile{"main": {URL: "http://127.0.0.1:1"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "http://example.com/proxy-probe", r.URL.String())
				w.WriteHeader(http.StatusOK)
			}))
			defer proxy.Close()
			cfg := config.DefaultConfig(nil, nil)
			cfg.Scrapers.Proxy = tc.persisted
			before, err := json.Marshal(cfg.Scrapers.Proxy)
			require.NoError(t, err)
			tokens := core.NewTokenStore()
			deps := newTestDeps(cfg, func(d *core.APIDeps) { d.TokenStore = tokens })
			rt := testkit.GetTestRuntime(deps)
			router := gin.New()
			router.POST("/proxy/test", testProxy(rt))
			requestProxy := models.ProxyConfig{Enabled: true, Profile: "main", Profiles: map[string]models.ProxyProfile{"main": {URL: proxy.URL}}}
			if tc.useDefault {
				requestProxy.Profile = ""
				requestProxy.DefaultProfile = "main"
			}
			body, err := json.Marshal(contracts.ProxyTestRequest{Mode: "direct", TargetURL: "http://example.com/proxy-probe", Proxy: requestProxy})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var response contracts.ProxyTestResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.True(t, response.Success, response.Message)
			requestProxy.Profile = ""
			requestProxy.DefaultProfile = "main"
			hash, err := core.HashProxyConfig(requestProxy)
			require.NoError(t, err)
			require.True(t, tokens.Validate(response.VerificationToken, "global", hash))
			after, err := json.Marshal(rt.GetAPIConfig().ProxyConfig)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
		})
	}
}
