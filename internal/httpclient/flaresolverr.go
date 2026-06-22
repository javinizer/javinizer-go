package httpclient

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// ============================================
// FlareSolverr Integration
// ============================================

// FlareSolverr represents the FlareSolverr client
type FlareSolverr struct {
	client       *resty.Client
	baseURL      string
	timeout      time.Duration
	maxRetries   int
	sessionTTL   int
	sessions     sync.Map // session ID -> flareSolverrSession
	requestProxy *flareSolverrProxy

	persistentSessionMu sync.Mutex
	persistentSessionID string
}

// flareSolverrSession represents a FlareSolverr session
type flareSolverrSession struct {
	Token   string
	Created time.Time
	URLs    []string
	Cookies []http.Cookie
}

// flareSolverrProxy represents a per-request proxy configuration passed to FlareSolverr.
// This is used for the target URL request made by FlareSolverr, not for calls to FlareSolverr itself.
type flareSolverrProxy struct {
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// flareSolverrRequest represents a request to FlareSolverr
type flareSolverrRequest struct {
	Cmd               string             `json:"cmd"`                           // "request.get" or "sessions.create"
	URL               string             `json:"url"`                           // Target URL
	MaxTimeout        int                `json:"maxTimeout"`                    // Timeout in milliseconds (FlareSolverr expects ms)
	Session           string             `json:"session"`                       // Optional: reuse existing session
	SessionTTLMinutes int                `json:"session_ttl_minutes,omitempty"` // Optional: rotate existing session when older than TTL
	Proxy             *flareSolverrProxy `json:"proxy,omitempty"`               // Optional: proxy for target URL request
}

// flareSolverrResponse represents a FlareSolverr response
type flareSolverrResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		Response string `json:"response"`
		Cookies  []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"cookies"`
		UserAgent string `json:"userAgent"`
	} `json:"solution"`
	Session string `json:"session"`
}

// newFlareSolverr creates a new FlareSolverr client
func newFlareSolverr(cfg *models.FlareSolverrConfig) (*FlareSolverr, error) {
	// Validate config
	if cfg.URL == "" {
		return nil, fmt.Errorf("FlareSolverr URL is required")
	}

	client := NewRestyClientNoProxy(time.Duration(cfg.Timeout)*time.Second, cfg.MaxRetries)

	return &FlareSolverr{
		client:     client,
		baseURL:    cfg.URL,
		timeout:    time.Duration(cfg.Timeout) * time.Second,
		maxRetries: cfg.MaxRetries,
		sessionTTL: secondsToMinutes(cfg.SessionTTL),
	}, nil
}

// ResolveURL resolves a URL through FlareSolverr, returning HTML content and cookies.
// The mutex is held for the entire operation to ensure session reuse is safe from
// concurrent reset calls.
func (fs *FlareSolverr) ResolveURL(targetURL string) (string, []http.Cookie, error) {
	fs.persistentSessionMu.Lock()
	defer fs.persistentSessionMu.Unlock()

	sessionID, err := fs.getOrCreatePersistentSessionLocked()
	if err != nil {
		logging.Warnf("FlareSolverr: failed to create persistent session, using one-off request: %v", err)
		return fs.resolveURLRequest(targetURL, "")
	}

	html, cookies, err := fs.ResolveURLWithSession(targetURL, sessionID)
	if err == nil {
		return html, cookies, nil
	}

	// Session can become invalid if FlareSolverr restarts or rotates storage.
	// Reset local session, recreate once, then retry.
	logging.Warnf("FlareSolverr: persistent session %s failed, recreating: %v", sessionID, err)
	fs.resetPersistentSessionLocked(sessionID)

	retrySessionID, retryErr := fs.getOrCreatePersistentSessionLocked()
	if retryErr != nil {
		logging.Warnf("FlareSolverr: failed to recreate persistent session, using one-off request: %v", retryErr)
		return fs.resolveURLRequest(targetURL, "")
	}

	html, cookies, err = fs.ResolveURLWithSession(targetURL, retrySessionID)
	if err == nil {
		return html, cookies, nil
	}

	logging.Warnf("FlareSolverr: recreated session %s failed, using one-off request: %v", retrySessionID, err)
	return fs.resolveURLRequest(targetURL, "")
}

// getOrCreatePersistentSessionLocked gets or creates a persistent session.
// Caller must hold persistentSessionMu.
func (fs *FlareSolverr) getOrCreatePersistentSessionLocked() (string, error) {
	if fs.persistentSessionID != "" {
		return fs.persistentSessionID, nil
	}

	sessionID, err := fs.CreateSession()
	if err != nil {
		return "", err
	}

	fs.persistentSessionID = sessionID
	return sessionID, nil
}

// resetPersistentSessionLocked clears the persistent session and destroys it.
// Caller must hold persistentSessionMu.
func (fs *FlareSolverr) resetPersistentSessionLocked(sessionID string) {
	if sessionID == "" {
		return
	}

	// Clear local persistent session ID
	if fs.persistentSessionID == sessionID {
		fs.persistentSessionID = ""
	}

	// Destroy session via HTTP (lock is held by caller, but DestroySession doesn't acquire it)
	if err := fs.DestroySession(sessionID); err != nil {
		logging.Debugf("FlareSolverr: session destroy during reset failed for %s: %v", sessionID, err)
	}

	// Remove from local cache
	fs.sessions.Delete(sessionID)
}

func (fs *FlareSolverr) resolveURLRequest(targetURL, sessionID string) (string, []http.Cookie, error) {
	req := flareSolverrRequest{
		Cmd:        "request.get",
		URL:        targetURL,
		MaxTimeout: int(fs.timeout.Milliseconds()),
	}
	if sessionID != "" {
		req.Session = sessionID
		if fs.sessionTTL > 0 {
			req.SessionTTLMinutes = fs.sessionTTL
		}
	}
	if fs.requestProxy != nil {
		req.Proxy = fs.requestProxy
	}

	var resp flareSolverrResponse
	result, err := fs.client.R().
		SetBody(req).
		SetResult(&resp).
		Post(fs.baseURL)

	if err != nil {
		return "", nil, fmt.Errorf("FlareSolverr request failed: %w", err)
	}

	if result.StatusCode() != 200 {
		return "", nil, fmt.Errorf("FlareSolverr returned status %d", result.StatusCode())
	}

	if resp.Status != "ok" {
		return "", nil, fmt.Errorf("FlareSolverr error: %s", resp.Message)
	}

	cookies := convertFlareSolverrCookies(resp)

	// Cache cookies in session for reuse on subsequent requests
	if sessionID != "" && len(cookies) > 0 {
		if s, ok := fs.sessions.Load(sessionID); ok {
			if session, ok := s.(*flareSolverrSession); ok {
				session.Cookies = cookies
			}
		}
	}

	return resp.Solution.Response, cookies, nil
}

// CreateSession creates a new FlareSolverr session for cookie persistence
func (fs *FlareSolverr) CreateSession() (string, error) {
	req := flareSolverrRequest{
		Cmd: "sessions.create",
	}
	if fs.requestProxy != nil {
		req.Proxy = fs.requestProxy
	}

	var resp flareSolverrResponse
	_, err := fs.client.R().
		SetBody(req).
		SetResult(&resp).
		Post(fs.baseURL)

	if err != nil {
		return "", fmt.Errorf("failed to create FlareSolverr session: %w", err)
	}

	if resp.Status != "ok" || resp.Session == "" {
		return "", fmt.Errorf("failed to create FlareSolverr session: %s", resp.Message)
	}

	// Store session info
	session := &flareSolverrSession{
		Token:   resp.Session,
		Created: time.Now(),
		URLs:    []string{},
	}
	fs.sessions.Store(resp.Session, session)

	return resp.Session, nil
}

// DestroySession destroys a FlareSolverr session via HTTP and removes it from local cache.
func (fs *FlareSolverr) DestroySession(sessionID string) error {
	req := flareSolverrRequest{
		Cmd:     "sessions.destroy",
		Session: sessionID,
	}

	var resp flareSolverrResponse
	_, err := fs.client.R().
		SetBody(req).
		SetResult(&resp).
		Post(fs.baseURL)

	// Remove from local cache regardless of API result
	fs.sessions.Delete(sessionID)

	if err != nil {
		return fmt.Errorf("failed to destroy FlareSolverr session: %w", err)
	}

	return nil
}

// ResolveURLWithSession resolves a URL using a specific session
func (fs *FlareSolverr) ResolveURLWithSession(targetURL, sessionID string) (string, []http.Cookie, error) {
	html, cookies, err := fs.resolveURLRequest(targetURL, sessionID)
	if err != nil {
		return "", nil, fmt.Errorf("FlareSolverr request with session failed: %w", err)
	}

	// Update session URLs
	if s, ok := fs.sessions.Load(sessionID); ok {
		if session, ok := s.(*flareSolverrSession); ok {
			session.URLs = append(session.URLs, targetURL)
		}
	}

	// Return cached cookies if response didn't include any
	if len(cookies) == 0 && sessionID != "" {
		if s, ok := fs.sessions.Load(sessionID); ok {
			if session, ok := s.(*flareSolverrSession); ok {
				cookies = session.Cookies
			}
		}
	}

	return html, cookies, nil
}

// Close cleans up resources held by the FlareSolverr.
// Currently a no-op since FlareSolverr uses stateless HTTP requests,
// but provides a cleanup hook for future resource management.
func (fs *FlareSolverr) Close() error {
	return nil
}

// buildFlareSolverrRequestProxy creates a flareSolverrProxy from a ProxyProfile.
// If the profile is nil or has no URL, returns nil.
func buildFlareSolverrRequestProxy(proxyProfile *models.ProxyProfile) *flareSolverrProxy {
	if proxyProfile == nil || proxyProfile.URL == "" {
		return nil
	}

	proxyURL := normalizeProxyURL(proxyProfile.URL)
	username := proxyProfile.Username
	password := proxyProfile.Password

	// If credentials are embedded in the URL and explicit credentials are absent,
	// extract and move them to separate fields.
	parsed, err := url.Parse(proxyURL)
	if err == nil {
		if parsed.User != nil {
			if username == "" {
				username = parsed.User.Username()
			}
			if password == "" {
				if p, ok := parsed.User.Password(); ok {
					password = p
				}
			}
			parsed.User = nil
			proxyURL = parsed.String()
		}
	}

	return &flareSolverrProxy{
		URL:      proxyURL,
		Username: username,
		Password: password,
	}
}

func secondsToMinutes(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 59) / 60
}

func convertFlareSolverrCookies(resp flareSolverrResponse) []http.Cookie {
	cookies := make([]http.Cookie, len(resp.Solution.Cookies))
	for i, c := range resp.Solution.Cookies {
		cookies[i] = http.Cookie{
			Name:  c.Name,
			Value: c.Value,
		}
	}
	return cookies
}
