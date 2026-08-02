package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/auth"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectSSRState_AuthNotInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rt := &core.APIRuntime{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/browse", nil)
	html := []byte(`<html><head><title>Test</title></head><body></body></html>`)
	result := injectSSRState(html, rt, c)
	assert.Contains(t, string(result), "window.__JAVINIZER_SSR__")
	assert.Contains(t, string(result), `"authStatus":null`)
}

func TestInjectSSRState_NilRuntime_ReturnsOriginalHTML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/browse", nil)
	html := []byte(`<html><head></head><body></body></html>`)
	result := injectSSRState(html, nil, c)
	assert.Equal(t, html, result)
}

func TestInjectSSRState_WithBrowseBootstrapCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rt := &core.APIRuntime{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/browse", nil)
	c.Request.AddCookie(&http.Cookie{Name: "javinizer_browse_bootstrap", Value: url.QueryEscape(`{"version":1,"applyPlan":{"version":1,"video_operation":"metadata-artwork","nfo_output":"skip","media_policy":"missing"},"initialPath":"/videos","destinationPath":"/videos","forceRefresh":false,"showScraperSelector":false,"selectedScrapers":[],"manualScrapeMode":false,"planExpanded":true}`)})
	html := []byte(`<html><head></head><body></body></html>`)
	result := injectSSRState(html, rt, c)
	marker := "window.__JAVINIZER_SSR__="
	idx := strings.Index(string(result), marker)
	require.GreaterOrEqual(t, idx, 0)
	var state struct {
		AuthStatus      json.RawMessage `json:"authStatus"`
		BrowseBootstrap json.RawMessage `json:"browseBootstrap,omitempty"`
	}
	jsonEnd := idx + len(marker)
	for jsonEnd < len(result) && result[jsonEnd] != ';' {
		jsonEnd++
	}
	require.NoError(t, json.Unmarshal(result[idx+len(marker):jsonEnd], &state))
	assert.NotNil(t, state.BrowseBootstrap)
	assert.Contains(t, string(state.BrowseBootstrap), "/videos")
}

func TestDecodeBrowseBootstrapCookie_NullApplyPlan(t *testing.T) {
	// Input arrives already URL-decoded via gin's Context.Cookie() — pass the
	// JSON body directly (the old double-decode contract used QueryEscape).
	decoded, ok := decodeBrowseBootstrapCookie(`{"version":1,"applyPlan":null,"initialPath":"/videos","destinationPath":"/videos","forceRefresh":false,"showScraperSelector":false,"selectedScrapers":[],"manualScrapeMode":false,"planExpanded":true}`)
	require.True(t, ok)
	assert.Contains(t, string(decoded), `"applyPlan":null`)
}

func TestInjectSSRState_InvalidBrowseBootstrapCookieIsIgnored(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "missing selected scrapers", value: `%7B%22version%22%3A1%2C%22initialPath%22%3A%22%2Fvideos%22%7D`},
		{name: "malformed JSON", value: `%7B`},
		{name: "wrong version", value: `%7B%22version%22%3A2%2C%22selectedScrapers%22%3A%5B%5D%7D`},
		{name: "wrong selected scraper type", value: `%7B%22version%22%3A1%2C%22selectedScrapers%22%3A%5B1%5D%7D`},
		{name: "wrong initial path type", value: url.QueryEscape(`{"version":1,"applyPlan":null,"initialPath":123,"destinationPath":"/videos","forceRefresh":false,"showScraperSelector":false,"selectedScrapers":[],"manualScrapeMode":false,"planExpanded":true}`)},
		{name: "wrong apply plan type", value: url.QueryEscape(`{"version":1,"applyPlan":123,"initialPath":"/videos","destinationPath":"/videos","forceRefresh":false,"showScraperSelector":false,"selectedScrapers":[],"manualScrapeMode":false,"planExpanded":true}`)},
		{name: "invalid apply plan", value: url.QueryEscape(`{"version":1,"applyPlan":{"version":1,"video_operation":"bad","nfo_output":"write","media_policy":"missing"},"initialPath":"/videos","destinationPath":"/videos","forceRefresh":false,"showScraperSelector":false,"selectedScrapers":[],"manualScrapeMode":false,"planExpanded":true}`)},
		{name: "invalid escape", value: `%zz`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rt := &core.APIRuntime{}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/browse", nil)
			c.Request.Header.Set("Cookie", browseBootstrapCookie+"="+tt.value)
			result := injectSSRState([]byte(`<html><head></head></html>`), rt, c)
			assert.NotContains(t, string(result), `"browseBootstrap"`)
		})
	}
}

func TestDecodeBrowseBootstrapCookie_InvalidInput(t *testing.T) {
	// Not JSON at all (post-decode junk) must be rejected, not crash.
	decoded, ok := decodeBrowseBootstrapCookie("%zz")
	assert.Nil(t, decoded)
	assert.False(t, ok)
}

func TestInjectSSRState_BrowseBootstrapPreservesPlusAndPercentPaths(t *testing.T) {
	// Regression: the cookie value must be URL-decoded EXACTLY ONCE. Gin's
	// Context.Cookie() already unescapes; a second QueryUnescape turned
	// "/media/A+B" into "/media/A B" and mis-decoded literal "%" sequences —
	// production reloads then hydrated a different browse/destination path
	// than the user left.
	gin.SetMode(gin.TestMode)
	rt := &core.APIRuntime{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/browse", nil)
	payload := `{"version":1,"applyPlan":null,"initialPath":"/media/A+B/50%done","destinationPath":"/out/C+D","forceRefresh":false,"showScraperSelector":false,"selectedScrapers":[],"manualScrapeMode":false,"planExpanded":true}`
	req.Header.Set("Cookie", browseBootstrapCookie+"="+url.QueryEscape(payload))
	c.Request = req

	result := string(injectSSRState([]byte(`<html><head></head></html>`), rt, c))
	assert.Contains(t, result, `"initialPath":"/media/A+B/50%done"`)
	assert.Contains(t, result, `"destinationPath":"/out/C+D"`)
}

func TestInjectSSRState_NoHeadMarker_ReturnsOriginalHTML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rt := &core.APIRuntime{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/browse", nil)
	html := []byte(`<html><body>no head marker</body></html>`)
	result := injectSSRState(html, rt, c)
	assert.Equal(t, html, result)
}

func TestResolveSSRAuth_NilRuntime(t *testing.T) {
	assert.Nil(t, resolveSSRAuth(nil, nil))
}

func TestInjectSSRState_UninitializedAuthManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := t.TempDir() + "/config.yaml"
	deps := createTestDeps(t, cfg, configFile)
	manager, err := auth.NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	deps.Auth = manager
	rt := core.NewAPIRuntime(deps)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/browse", nil)
	result := injectSSRState([]byte(`<html><head></head></html>`), rt, c)
	assert.Contains(t, string(result), `"initialized":false`)
	assert.Contains(t, string(result), `"authenticated":false`)
}

func TestInjectSSRState_InitializedAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	configFile := t.TempDir() + "/config.yaml"
	deps := createTestDeps(t, cfg, configFile)
	manager, err := auth.NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	deps.Auth = manager
	rt := core.NewAPIRuntime(deps)

	newContext := func(cookie string) *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/browse", nil)
		if cookie != "" {
			c.Request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
		}
		return c
	}

	unauthenticated := injectSSRState([]byte(`<html><head></head></html>`), rt, newContext(""))
	assert.Contains(t, string(unauthenticated), `"initialized":true`)
	assert.Contains(t, string(unauthenticated), `"authenticated":false`)

	invalid := injectSSRState([]byte(`<html><head></head></html>`), rt, newContext("invalid-session"))
	assert.Contains(t, string(invalid), `"authenticated":false`)

	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)
	authenticated := injectSSRState([]byte(`<html><head></head></html>`), rt, newContext(sessionID))
	assert.Contains(t, string(authenticated), `"authenticated":true`)
	assert.Contains(t, string(authenticated), `"username":"admin"`)
}
