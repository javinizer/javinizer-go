package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
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
	c.Request.AddCookie(&http.Cookie{Name: "javinizer_browse_bootstrap", Value: `%7B%22version%22%3A1%2C%22initialPath%22%3A%22%2Fvideos%22%7D`})
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
