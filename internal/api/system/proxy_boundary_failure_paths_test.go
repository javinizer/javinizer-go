package system

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/javinizer/javinizer-go/internal/testutil/ssrffixture"
	"github.com/stretchr/testify/require"
)

type unavailableVerificationTokenStore struct {
	core.TokenStoreInterface
	calls       int
	scope, hash string
}

func (s *unavailableVerificationTokenStore) Create(scope, hash string) (string, time.Time, error) {
	s.calls++
	s.scope = scope
	s.hash = hash
	return "", time.Time{}, errors.New("token store unavailable")
}

func TestProxySuccessfulConnectionDoesNotInventFailedToken(t *testing.T) {
	t.Cleanup(ssrf.SetLookupIPForTest(func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }))
	target := ssrffixture.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	proxy := startTestForwardProxy(t)
	defer proxy.Close()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy = models.ProxyConfig{Enabled: true, DefaultProfile: "main", Profiles: map[string]models.ProxyProfile{"main": {URL: proxy.URL}}}
	tokens := &unavailableVerificationTokenStore{}
	deps := newTestDeps(cfg, func(d *core.APIDeps) { d.TokenStore = tokens })
	payload := contracts.ProxyTestRequest{Mode: "direct", TargetURL: target.URL, Proxy: models.ProxyConfig{Enabled: true}}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy/test", strings.NewReader(string(raw)))
	c.Request.Header.Set("Content-Type", "application/json")
	testProxy(testkit.GetTestRuntime(deps))(c)
	require.Equal(t, http.StatusOK, rec.Code)
	var result contracts.ProxyTestResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.True(t, result.Success, "token failure must not misreport the successful connection")
	require.Empty(t, result.VerificationToken, "failed issuance must not grant save authorization")
	require.Zero(t, result.TokenExpiresAt)
	require.Equal(t, 1, tokens.calls)
	require.Equal(t, "global", tokens.scope)
	expected, err := core.HashProxyConfig(payload.Proxy)
	require.NoError(t, err)
	require.Equal(t, expected, tokens.hash)
}

func TestProxyRejectsUnknownModeBeforeConnection(t *testing.T) {
	t.Cleanup(ssrf.SetLookupIPForTest(func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }))
	var requests atomic.Int64
	target := ssrffixture.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer target.Close()
	deps := newTestDeps(config.DefaultConfig(nil, nil))
	raw, err := json.Marshal(contracts.ProxyTestRequest{Mode: "unknown", TargetURL: target.URL})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy/test", strings.NewReader(string(raw)))
	c.Request.Header.Set("Content-Type", "application/json")
	testProxy(testkit.GetTestRuntime(deps))(c)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid proxy test request")
	require.Zero(t, requests.Load(), "invalid mode must be rejected before contacting the target")
}
