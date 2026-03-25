package httpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"golang.org/x/net/proxy"
)

// SanitizeProxyURL removes credentials from proxy URL for safe logging
func SanitizeProxyURL(proxyURL string) string {
	u, err := url.Parse(normalizeProxyURL(proxyURL))
	if err != nil {
		return proxyURL // Return as-is if unparseable
	}
	if u.User != nil {
		// Replace user info with [REDACTED]
		u.User = url.User("[REDACTED]")
	}
	return u.String()
}

func normalizeProxyURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		return trimmed
	}
	// Allow host:port inputs by defaulting to HTTP proxy scheme.
	return "http://" + trimmed
}

// NewTransport creates an http.Transport with optional proxy support
func NewTransport(proxyConfig *config.ProxyConfig) (*http.Transport, error) {
	// Clone default transport to preserve Go's safety timeouts
	// (DialContext timeout, TLSHandshakeTimeout, ExpectContinueTimeout, etc.)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Enforce config-only proxy behavior: never inherit HTTP(S)_PROXY from environment.
	transport.Proxy = nil

	if proxyConfig != nil && proxyConfig.Enabled && proxyConfig.URL != "" {
		parsedProxyURL, err := url.Parse(normalizeProxyURL(proxyConfig.URL))
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}

		// Handle authentication
		if proxyConfig.Username != "" && proxyConfig.Password != "" {
			parsedProxyURL.User = url.UserPassword(proxyConfig.Username, proxyConfig.Password)
		}

		// Check if SOCKS5
		if parsedProxyURL.Scheme == "socks5" {
			// Use golang.org/x/net/proxy for SOCKS5
			var auth *proxy.Auth
			if proxyConfig.Username != "" && proxyConfig.Password != "" {
				auth = &proxy.Auth{
					User:     proxyConfig.Username,
					Password: proxyConfig.Password,
				}
			}
			dialer, err := proxy.SOCKS5("tcp", parsedProxyURL.Host, auth, proxy.Direct)
			if err != nil {
				return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
			}
			// Use DialContext to honor request cancellation and timeouts
			// Check if dialer supports DialContext (it does - proxy.Dialer implements ContextDialer)
			if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
				transport.DialContext = contextDialer.DialContext
			} else {
				// Fallback: wrap Dial with context (shouldn't happen with proxy.SOCKS5)
				transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				}
			}
			// Clear transport.Proxy to prevent HTTP_PROXY env vars from overriding SOCKS5
			transport.Proxy = nil
		} else {
			// HTTP/HTTPS proxy
			transport.Proxy = http.ProxyURL(parsedProxyURL)
		}
	}

	return transport, nil
}

// NewHTTPClient creates a standard http.Client with proxy support
func NewHTTPClient(proxyConfig *config.ProxyConfig, timeout time.Duration) (*http.Client, error) {
	transport, err := NewTransport(proxyConfig)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}

// NewRestyClient creates a resty.Client with proxy support
func NewRestyClient(proxyConfig *config.ProxyConfig, timeout time.Duration, retries int) (*resty.Client, error) {
	transport, err := NewTransport(proxyConfig)
	if err != nil {
		return nil, err
	}

	client := resty.New()
	client.SetTimeout(timeout)
	client.SetRetryCount(retries)
	client.SetTransport(transport)

	return client, nil
}

// NewRestyClientNoProxy creates a resty.Client that explicitly bypasses
// environment proxy variables by using a no-proxy transport.
func NewRestyClientNoProxy(timeout time.Duration, retries int) *resty.Client {
	client := resty.New()
	client.SetTimeout(timeout)
	client.SetRetryCount(retries)

	transport, err := NewTransport(nil)
	if err != nil {
		logging.Warnf("HTTP client: failed to create explicit no-proxy transport, using Resty default transport: %v", err)
		return client
	}

	client.SetTransport(transport)
	return client
}

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
	sessions     sync.Map // session ID -> FlareSolverrSession
	requestProxy *FlareSolverrProxy

	persistentSessionMu sync.Mutex
	persistentSessionID string
}

// FlareSolverrSession represents a FlareSolverr session
type FlareSolverrSession struct {
	Token   string
	Created time.Time
	URLs    []string
}

// FlareSolverrProxy represents a per-request proxy configuration passed to FlareSolverr.
// This is used for the target URL request made by FlareSolverr, not for calls to FlareSolverr itself.
type FlareSolverrProxy struct {
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// FlareSolverrRequest represents a request to FlareSolverr
type FlareSolverrRequest struct {
	Cmd               string             `json:"cmd"`                           // "request.get" or "sessions.create"
	URL               string             `json:"url"`                           // Target URL
	MaxTimeout        int                `json:"maxTimeout"`                    // Timeout in milliseconds (FlareSolverr expects ms)
	Session           string             `json:"session"`                       // Optional: reuse existing session
	SessionTTLMinutes int                `json:"session_ttl_minutes,omitempty"` // Optional: rotate existing session when older than TTL
	Proxy             *FlareSolverrProxy `json:"proxy,omitempty"`               // Optional: proxy for target URL request
}

// FlareSolverrResponse represents a FlareSolverr response
type FlareSolverrResponse struct {
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

// NewFlareSolverr creates a new FlareSolverr client
func NewFlareSolverr(cfg *config.FlareSolverrConfig) (*FlareSolverr, error) {
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

// ResolveURL resolves a URL through FlareSolverr, returning HTML content and cookies
func (fs *FlareSolverr) ResolveURL(targetURL string) (string, []http.Cookie, error) {
	sessionID, err := fs.getOrCreatePersistentSession()
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
	fs.resetPersistentSession(sessionID)

	retrySessionID, retryErr := fs.getOrCreatePersistentSession()
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

func (fs *FlareSolverr) getOrCreatePersistentSession() (string, error) {
	fs.persistentSessionMu.Lock()
	defer fs.persistentSessionMu.Unlock()

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

func (fs *FlareSolverr) resetPersistentSession(sessionID string) {
	fs.persistentSessionMu.Lock()
	if fs.persistentSessionID == sessionID {
		fs.persistentSessionID = ""
	}
	fs.persistentSessionMu.Unlock()

	if sessionID == "" {
		return
	}
	if err := fs.DestroySession(sessionID); err != nil {
		logging.Debugf("FlareSolverr: session destroy during reset failed for %s: %v", sessionID, err)
	}
}

func (fs *FlareSolverr) resolveURLRequest(targetURL, sessionID string) (string, []http.Cookie, error) {
	req := FlareSolverrRequest{
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

	var resp FlareSolverrResponse
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

	return resp.Solution.Response, convertFlareSolverrCookies(resp), nil
}

// CreateSession creates a new FlareSolverr session for cookie persistence
func (fs *FlareSolverr) CreateSession() (string, error) {
	req := FlareSolverrRequest{
		Cmd: "sessions.create",
	}
	if fs.requestProxy != nil {
		req.Proxy = fs.requestProxy
	}

	var resp FlareSolverrResponse
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
	session := &FlareSolverrSession{
		Token:   resp.Session,
		Created: time.Now(),
		URLs:    []string{},
	}
	fs.sessions.Store(resp.Session, session)

	return resp.Session, nil
}

// DestroySession destroys a FlareSolverr session
func (fs *FlareSolverr) DestroySession(sessionID string) error {
	req := FlareSolverrRequest{
		Cmd:     "sessions.destroy",
		Session: sessionID,
	}

	fs.persistentSessionMu.Lock()
	if fs.persistentSessionID == sessionID {
		fs.persistentSessionID = ""
	}
	fs.persistentSessionMu.Unlock()

	var resp FlareSolverrResponse
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
		if session, ok := s.(*FlareSolverrSession); ok {
			session.URLs = append(session.URLs, targetURL)
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

// NewRestyClientWithFlareSolverr creates a resty.Client with optional FlareSolverr support
func NewRestyClientWithFlareSolverr(proxyConfig *config.ProxyConfig, timeout time.Duration, retries int) (*resty.Client, *FlareSolverr, error) {
	client, err := NewRestyClient(proxyConfig, timeout, retries)
	if err != nil {
		return nil, nil, err
	}

	// If no proxy config was provided, return plain client without FlareSolverr.
	if proxyConfig == nil {
		return client, nil, nil
	}

	// If FlareSolverr is enabled, create a client
	var fs *FlareSolverr
	if proxyConfig.FlareSolverr.Enabled {
		fs, err = NewFlareSolverr(&proxyConfig.FlareSolverr)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create FlareSolverr client: %w", err)
		}
		fs.requestProxy = buildFlareSolverrRequestProxy(proxyConfig)
		if fs.requestProxy != nil {
			logging.Infof("FlareSolverr request proxy enabled: %s", SanitizeProxyURL(fs.requestProxy.URL))
		}
		logging.Infof("FlareSolverr enabled at %s", proxyConfig.FlareSolverr.URL)
	}

	return client, fs, nil
}

// GetFlareSolverrFromClient extracts FlareSolverr instance from resty client context
// Note: This is a helper for scrapers that need to access FlareSolverr
// The FlareSolverr instance is typically stored separately and passed to scrapers
func GetFlareSolverrFromClient(client *resty.Client) (*FlareSolverr, bool) {
	// The FlareSolverr is not stored in the client context directly
	// Instead, scrapers receive it via their constructor
	// This function is kept for API consistency but returns false
	return nil, false
}

func buildFlareSolverrRequestProxy(proxyConfig *config.ProxyConfig) *FlareSolverrProxy {
	if proxyConfig == nil || !proxyConfig.Enabled || proxyConfig.URL == "" {
		return nil
	}

	proxyURL := normalizeProxyURL(proxyConfig.URL)
	username := proxyConfig.Username
	password := proxyConfig.Password

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

	return &FlareSolverrProxy{
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

func convertFlareSolverrCookies(resp FlareSolverrResponse) []http.Cookie {
	cookies := make([]http.Cookie, len(resp.Solution.Cookies))
	for i, c := range resp.Solution.Cookies {
		cookies[i] = http.Cookie{
			Name:  c.Name,
			Value: c.Value,
		}
	}
	return cookies
}
