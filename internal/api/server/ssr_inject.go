package server

import (
	"bytes"
	"encoding/json"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/javinizer/javinizer-go/internal/api/core"
)

const (
	sessionCookieName     = "javinizer_session"
	browseBootstrapCookie = "javinizer_browse_bootstrap"
	ssrInjectMarker       = "</head>"
)

type ssrAuthStatus struct {
	Initialized   bool   `json:"initialized"`
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
}

type ssrState struct {
	AuthStatus      *ssrAuthStatus  `json:"authStatus"`
	BrowseBootstrap json.RawMessage `json:"browseBootstrap,omitempty"`
}

// injectSSRState injects a <script>window.__JAVINIZER_SSR__=...</script> tag
// before </head> in the SPA index.html. This allows the production static SPA
// (served by Gin without a Node SSR runtime) to read auth and browse-bootstrap
// state from the injected script, eliminating flash-of-content on full reloads.
// In dev mode, SvelteKit's own SSR provides this via +layout.server.ts; this
// injection is the production equivalent for the adapter-static SPA fallback.
func injectSSRState(html []byte, rt *core.APIRuntime, c *gin.Context) []byte {
	if rt == nil {
		return html
	}
	state := ssrState{AuthStatus: resolveSSRAuth(rt, c)}

	if raw := readCookieValue(c, browseBootstrapCookie); raw != "" {
		if decoded, decErr := url.QueryUnescape(raw); decErr == nil && json.Valid([]byte(decoded)) {
			state.BrowseBootstrap = json.RawMessage(decoded)
		}
	}

	payload, _ := json.Marshal(state)

	script := []byte("<script>window.__JAVINIZER_SSR__=" + string(payload) + ";</script>")
	idx := bytes.Index(html, []byte(ssrInjectMarker))
	if idx == -1 {
		return html
	}
	result := make([]byte, 0, len(html)+len(script))
	result = append(result, html[:idx]...)
	result = append(result, script...)
	result = append(result, html[idx:]...)
	return result
}

func resolveSSRAuth(rt *core.APIRuntime, c *gin.Context) *ssrAuthStatus {
	if rt == nil {
		return nil
	}
	deps := rt.Deps()
	if deps == nil || deps.Auth == nil {
		return nil
	}
	if !deps.Auth.IsInitialized() {
		return &ssrAuthStatus{Initialized: false, Authenticated: false}
	}
	status := &ssrAuthStatus{Initialized: true, Authenticated: false}
	sessionID := readCookieValue(c, sessionCookieName)
	if sessionID == "" {
		return status
	}
	username, authErr := deps.Auth.AuthenticateSession(sessionID)
	if authErr == nil {
		status.Authenticated = true
		status.Username = username
	}
	return status
}

func readCookieValue(c *gin.Context, name string) string {
	cookie, err := c.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie
}
