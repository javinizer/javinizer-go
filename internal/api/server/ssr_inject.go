package server

import (
	"bytes"
	"encoding/json"

	"github.com/gin-gonic/gin"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/applyplan"
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

type browseBootstrapCookiePayload struct {
	Version              *int            `json:"version"`
	ApplyPlan            json.RawMessage `json:"applyPlan"`
	PlanMigrationWarning *string         `json:"planMigrationWarning"`
	InitialPath          *string         `json:"initialPath"`
	DestinationPath      *string         `json:"destinationPath"`
	ForceRefresh         *bool           `json:"forceRefresh"`
	ShowScraperSelector  *bool           `json:"showScraperSelector"`
	SelectedScrapers     *[]string       `json:"selectedScrapers"`
	ManualScrapeMode     *bool           `json:"manualScrapeMode"`
	PlanExpanded         *bool           `json:"planExpanded"`
}

type normalizedBrowseBootstrap struct {
	Version              int             `json:"version"`
	ApplyPlan            *applyplan.Plan `json:"applyPlan"`
	PlanMigrationWarning *string         `json:"planMigrationWarning,omitempty"`
	InitialPath          string          `json:"initialPath"`
	DestinationPath      string          `json:"destinationPath"`
	ForceRefresh         bool            `json:"forceRefresh"`
	ShowScraperSelector  bool            `json:"showScraperSelector"`
	SelectedScrapers     []string        `json:"selectedScrapers"`
	ManualScrapeMode     bool            `json:"manualScrapeMode"`
	PlanExpanded         bool            `json:"planExpanded"`
}

// injectSSRState injects a <script>window.__JAVINIZER_SSR__=...</script> tag
// before </head> in the SPA index.html. This allows the production static SPA
// (served by Gin without a Node SSR runtime) to read auth and browse-bootstrap
// state from the injected script, eliminating flash-of-content on full reloads.
// In dev mode, SvelteKit's SSR provides this via the universal +layout load;
// this injection is what the adapter-static SPA fallback consumes in production.
func injectSSRState(html []byte, rt *core.APIRuntime, c *gin.Context) []byte {
	if rt == nil {
		return html
	}
	state := ssrState{AuthStatus: resolveSSRAuth(rt, c)}

	if raw := readCookieValue(c, browseBootstrapCookie); raw != "" {
		if decoded, ok := decodeBrowseBootstrapCookie(raw); ok {
			state.BrowseBootstrap = decoded
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

// decodeBrowseBootstrapCookie parses the browse-bootstrap cookie body. The
// input must ALREADY be URL-decoded: readCookieValue pulls the value through
// gin's Context.Cookie(), which runs url.QueryUnescape internally. Unescaping
// a second time would corrupt legitimate paths — "/media/A+B" would become
// "/media/A B", and literal percent sequences could mis-decode or
// invalidate the cookie entirely.
func decodeBrowseBootstrapCookie(raw string) (json.RawMessage, bool) {
	var bootstrap browseBootstrapCookiePayload
	if err := json.Unmarshal([]byte(raw), &bootstrap); err != nil ||
		bootstrap.Version == nil || *bootstrap.Version != 1 ||
		len(bootstrap.ApplyPlan) == 0 || bootstrap.InitialPath == nil ||
		bootstrap.DestinationPath == nil || bootstrap.ForceRefresh == nil ||
		bootstrap.ShowScraperSelector == nil || bootstrap.SelectedScrapers == nil ||
		bootstrap.ManualScrapeMode == nil || bootstrap.PlanExpanded == nil {
		return nil, false
	}
	var plan *applyplan.Plan
	if !bytes.Equal(bytes.TrimSpace(bootstrap.ApplyPlan), []byte("null")) {
		var decodedPlan applyplan.Plan
		if err := json.Unmarshal(bootstrap.ApplyPlan, &decodedPlan); err != nil {
			return nil, false
		}
		normalizedPlan, nerr := applyplan.Normalize(&decodedPlan)
		if nerr != nil {
			return nil, false
		}
		plan = normalizedPlan
	}
	normalized, _ := json.Marshal(normalizedBrowseBootstrap{
		Version:              1,
		ApplyPlan:            plan,
		PlanMigrationWarning: bootstrap.PlanMigrationWarning,
		InitialPath:          *bootstrap.InitialPath,
		DestinationPath:      *bootstrap.DestinationPath,
		ForceRefresh:         *bootstrap.ForceRefresh,
		ShowScraperSelector:  *bootstrap.ShowScraperSelector,
		SelectedScrapers:     *bootstrap.SelectedScrapers,
		ManualScrapeMode:     *bootstrap.ManualScrapeMode,
		PlanExpanded:         *bootstrap.PlanExpanded,
	})
	return normalized, true
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
