package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This loopback fixture models TRAWL v1.9.0's HTTP envelope and optional-session
// validation, without starting TRAWL, a browser, or any target URL request.
// https://github.com/germondai/trawl/blob/c4210ea2b6a089ecf00a22f9e9b4f517b6b388ae/apps/api/src/validation.ts
func newFlareSolverrCompatFixture(t *testing.T, path string, createStatus int) (*httptest.Server, <-chan map[string]any) {
	t.Helper()
	requests := make(chan map[string]any, 16)
	validID := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		if raw, exists := body["session"]; exists {
			id, ok := raw.(string)
			if !ok || !validID.MatchString(id) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"status":"error","message":"session ID must contain 1-128 letters, numbers, underscores or hyphens","solution":{}}`)
				return
			}
		}
		switch body["cmd"] {
		case "sessions.create":
			w.WriteHeader(createStatus)
			if createStatus != http.StatusOK {
				fmt.Fprint(w, `{"status":"error","message":"session creation unavailable"}`)
				return
			}
			fmt.Fprint(w, `{"status":"ok","message":"Session is ready.","startTimestamp":1,"endTimestamp":2,"version":"2.0.0","session":"fixture-session"}`)
		case "request.get":
			fmt.Fprint(w, `{"status":"ok","message":"","startTimestamp":1,"endTimestamp":2,"version":"2.0.0","solution":{"url":"http://fixture.invalid/page","status":200,"headers":{},"response":"<html>fixture</html>","cookies":[{"name":"demo","value":"1","domain":"fixture.invalid","path":"/","expires":123,"httpOnly":true,"secure":false}],"userAgent":"fixture"}}`)
		case "sessions.destroy":
			fmt.Fprint(w, `{"status":"ok","message":"The session has been removed.","version":"2.0.0"}`)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	return server, requests
}

func newFlareSolverrCompatClient(t *testing.T, endpoint string) *FlareSolverr {
	t.Helper()
	fs, err := newFlareSolverr(&models.FlareSolverrConfig{
		URL: endpoint, Enabled: true, Timeout: 2, SessionTTL: 300,
	})
	require.NoError(t, err)
	return fs
}

func TestFlareSolverr_TrawlSessionCompatibility(t *testing.T) {
	for _, path := range []string{"/v1", "/gateway/v1"} {
		t.Run(path, func(t *testing.T) {
			server, requests := newFlareSolverrCompatFixture(t, path, http.StatusOK)
			fs := newFlareSolverrCompatClient(t, server.URL+path)
			fs.requestProxy = &flareSolverrProxy{URL: "http://proxy.invalid:8080", Username: "fixture-user", Password: "fixture-password"}
			for i := 0; i < 2; i++ {
				html, cookies, err := fs.ResolveURL("http://fixture.invalid/page")
				require.NoError(t, err)
				assert.Equal(t, "<html>fixture</html>", html)
				require.Len(t, cookies, 1)
				assert.Equal(t, "demo", cookies[0].Name)
				assert.Equal(t, "1", cookies[0].Value)
			}
			require.NoError(t, fs.DestroySession("fixture-session"))
			require.Len(t, requests, 4)
			create := <-requests
			assert.Equal(t, "sessions.create", create["cmd"])
			assert.NotContains(t, create, "session", "empty session IDs must be omitted, not serialized as empty strings")
			assert.Equal(t, map[string]any{"url": "http://proxy.invalid:8080", "username": "fixture-user", "password": "fixture-password"}, create["proxy"])
			for i := 0; i < 2; i++ {
				request := <-requests
				assert.Equal(t, "request.get", request["cmd"])
				assert.Equal(t, "fixture-session", request["session"])
				assert.Equal(t, "http://fixture.invalid/page", request["url"])
				assert.Equal(t, float64(5), request["session_ttl_minutes"])
				assert.Equal(t, float64(2000), request["maxTimeout"])
				assert.Equal(t, create["proxy"], request["proxy"])
			}
			destroy := <-requests
			assert.Equal(t, "sessions.destroy", destroy["cmd"])
			assert.Equal(t, "fixture-session", destroy["session"])
			_, cached := fs.sessions.Load("fixture-session")
			assert.False(t, cached)
		})
	}
}

func TestFlareSolverr_TrawlOneOffFallback(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server, requests := newFlareSolverrCompatFixture(t, "/v1", status)
			fs := newFlareSolverrCompatClient(t, server.URL+"/v1")
			html, _, err := fs.ResolveURL("http://fixture.invalid/page")
			require.NoError(t, err)
			assert.Equal(t, "<html>fixture</html>", html)
			require.Len(t, requests, 2)
			create := <-requests
			assert.Equal(t, "sessions.create", create["cmd"])
			assert.NotContains(t, create, "session")
			request := <-requests
			assert.Equal(t, "request.get", request["cmd"])
			assert.NotContains(t, request, "session")
			assert.NotContains(t, request, "session_ttl_minutes")
		})
	}
}

func TestFlareSolverr_HTTPStatusErrors(t *testing.T) {
	const secret = "upstream-must-not-echo-proxy-password"
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		truncated   bool
	}{
		{"bad_request_json", 400, "application/json", `{"status":"error","message":"` + secret + `"}`, false},
		{"not_found_html", 404, "text/html", "<html>" + secret + "</html>", false},
		{"capacity_plain_text", 429, "text/plain", secret, false},
		{"internal_error_malformed_json", 500, "application/json", "{" + secret, false},
		{"large_error_body", 503, "application/json", `{"message":"` + strings.Repeat(secret, 4096) + `"}`, false},
		{"unexpected_created", 201, "application/json", `{"status":"ok","session":"unexpected"}`, false},
		{"unexpected_empty_success", 204, "application/json", "", false},
		{"created_malformed_json", 201, "application/json", `{"status":`, false},
		{"accepted_invalid_type", 202, "application/json", `{"status":["ok"]}`, false},
		{"partial_malformed_json", 206, "application/json", `{"status":`, false},
		{"bad_request_truncated", 400, "application/json", `{"status":"error"}`, true},
		{"unavailable_truncated", 503, "application/json", `{"status":"error"}`, true},
		{"created_invalid_xml_entity", 201, "application/xml", `<response><status>&` + secret + `;</status></response>`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				if tt.truncated {
					w.Header().Set("Content-Length", "4096")
				}
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			fs := newFlareSolverrCompatClient(t, server.URL+"/v1")
			operations := []struct {
				name string
				run  func() error
			}{
				{"create", func() error { _, err := fs.CreateSession(); return err }},
				{"request", func() error {
					_, _, err := fs.ResolveURLWithSession("http://fixture.invalid/page", "fixture-session")
					return err
				}},
				{"destroy", func() error {
					fs.sessions.Store("fixture-session", &flareSolverrSession{})
					err := fs.DestroySession("fixture-session")
					_, cached := fs.sessions.Load("fixture-session")
					assert.False(t, cached, "failed destruction must still discard the local cache entry")
					return err
				}},
			}
			for _, operation := range operations {
				t.Run(operation.name, func(t *testing.T) {
					err := operation.run()
					require.Error(t, err)
					var httpErr *flareSolverrHTTPError
					require.True(t, errors.As(err, &httpErr), "%v", err)
					assert.Equal(t, tt.status, httpErr.StatusCode)
					assert.Contains(t, err.Error(), fmt.Sprintf("status %d", tt.status))
					assert.NotContains(t, err.Error(), secret)
					assert.NotContains(t, err.Error(), "fixture.invalid")
					assert.Less(t, len(err.Error()), 150)
				})
			}
		})
	}
}

func TestFlareSolverr_NoResponsePreservesErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request with a preflight error or expired context reached the server")
	}))
	defer server.Close()
	for _, mode := range []string{"preflight", "canceled", "deadline"} {
		for _, operation := range []string{"create", "request", "destroy"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				fs := newFlareSolverrCompatClient(t, server.URL+"/v1")
				want := errors.New("fixture preflight error")
				var ctx context.Context
				if mode == "canceled" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
					want = context.Canceled
				} else if mode == "deadline" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
					defer cancel()
					want = context.DeadlineExceeded
				}
				fs.client.OnBeforeRequest(func(_ *resty.Client, req *resty.Request) error {
					if ctx == nil {
						return want
					}
					req.SetContext(ctx)
					return nil
				})
				var err error
				switch operation {
				case "create":
					_, err = fs.CreateSession()
				case "request":
					_, _, err = fs.ResolveURLWithSession("http://fixture.invalid/page", "fixture-session")
				case "destroy":
					fs.sessions.Store("fixture-session", &flareSolverrSession{})
					err = fs.DestroySession("fixture-session")
					_, cached := fs.sessions.Load("fixture-session")
					assert.False(t, cached)
				}
				require.ErrorIs(t, err, want)
				var statusErr *flareSolverrHTTPError
				assert.False(t, errors.As(err, &statusErr), "must not fabricate an HTTP status")
			})
		}
	}
}

func TestFlareSolverr_SessionEnvelopeErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{"empty_status", `{}`, "response did not confirm success"},
		{"empty_error_message", `{"status":"error"}`, "response did not confirm success"},
		{"missing_session_id", `{"status":"ok"}`, "successful response is missing a session ID"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			fs := newFlareSolverrCompatClient(t, server.URL)
			_, err := fs.CreateSession()
			require.ErrorContains(t, err, tt.want)
			if tt.name != "missing_session_id" {
				err = fs.DestroySession("fixture-session")
				require.ErrorContains(t, err, "response did not confirm success")
			}
		})
	}
}
