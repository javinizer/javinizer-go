package actresssync

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	"github.com/javinizer/javinizer-go/internal/api/auth"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/token"
	"github.com/stretchr/testify/require"
)

func TestEveryRouteRequiresAuthentication(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/status", ""}, {"GET", "/sources", ""}, {"GET", "/jobs", ""}, {"GET", "/jobs/" + testJobID, ""}, {"GET", "/jobs/" + testJobID + "/tasks", ""}, {"POST", "/reviews", reviewBody}, {"POST", "/jobs", confirmBody}, {"POST", "/jobs/" + testJobID + "/cancel", cancelBody},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			f := newFixture(t)
			r := newRequest(tc.method, tc.path, tc.body)
			r.Header.Del("Authorization")
			w := f.serve(r)
			require.Equal(t, 401, w.Code, w.Body.String())
			require.Empty(t, f.service.recorded())
		})
	}
}

func TestPrincipalComesOnlyFromVerifiedAuthContext(t *testing.T) {
	cases := []struct{ name, authorization, cookie, header, want string }{
		{"bearer", "Bearer " + token.TokenPrefix + "alice", "", "", "token:token-alice"},
		{"second token", "bEaReR " + token.TokenPrefix + "bob", "", "", "token:token-bob"},
		{"cookie", "", "session-alice", "", "session:alice"},
		{"rotated session", "", "session-alice-rotated", "", "session:alice"},
		{"session header", "", "", "session-bob", "session:bob"},
		{"cookie precedence", "", "session-alice", "session-bob", "session:alice"},
		{"token precedence", "Bearer " + token.TokenPrefix + "bob", "session-alice", "session-alice", "token:token-bob"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			r := newRequest("POST", "/reviews", reviewBody)
			r.Header.Set("Authorization", tc.authorization)
			if tc.cookie != "" {
				r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tc.cookie})
			}
			if tc.header != "" {
				r.Header.Set("X-Session-ID", tc.header)
			}
			r.Header.Set("X-Principal", "session:attacker")
			r.Header.Set("token_id", "attacker")
			r.Header.Set("auth_username", "attacker")
			w := f.serve(r)
			require.Equal(t, 201, w.Code, w.Body.String())
			require.Equal(t, tc.want, f.service.recorded()[0].principal)
		})
	}
}

func TestQueryOnlySessionAndInvalidBearerCannotGainAuthority(t *testing.T) {
	for _, tc := range []struct{ name, path, authorization, cookie, header string }{
		{"query session", "/reviews?session=session-alice", "", "", ""},
		{"query session read", "/status?session=session-alice", "", "", ""},
		{"invalid bearer with cookie", "/reviews", "Bearer invalid", "session-alice", ""},
		{"revoked bearer with cookie", "/reviews", "Bearer " + token.TokenPrefix + "revoked", "session-alice", ""},
		{"invalid cookie with valid header", "/reviews", "", "invalid", "session-alice"},
		{"invalid header with valid query", "/reviews?session=session-alice", "", "", "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			method := "POST"
			if strings.HasPrefix(tc.path, "/status") {
				method = "GET"
			}
			r := newRequest(method, tc.path, reviewBody)
			r.Header.Set("Authorization", tc.authorization)
			if tc.cookie != "" {
				r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tc.cookie})
			}
			if tc.header != "" {
				r.Header.Set("X-Session-ID", tc.header)
			}
			w := f.serve(r)
			require.Equal(t, 401, w.Code, w.Body.String())
			require.Empty(t, f.service.recorded())
		})
	}
}

func TestIncompleteAuthContextFailsClosed(t *testing.T) {
	for _, tc := range []struct{ method, username, tokenID string }{
		{"", "alice", ""}, {"token", "api_token", ""}, {"session", "", ""}, {"unrecognized", "alice", "token-alice"},
	} {
		t.Run(tc.method+tc.username, func(t *testing.T) {
			f := newFixture(t)
			router := gin.New()
			group := router.Group("/api/v1", func(c *gin.Context) {
				c.Set("auth_method", tc.method)
				c.Set("auth_username", tc.username)
				c.Set("token_id", tc.tokenID)
			})
			f.handler.register(group, group)
			r := newRequest("POST", "/reviews", reviewBody)
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-alice"})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			require.Equal(t, 401, w.Code)
			require.Empty(t, f.service.recorded())
		})
	}
}

func TestConfirmationCannotBorrowAnotherPrincipal(t *testing.T) {
	f := newFixture(t)
	var reviewedBy string
	f.service.hook = func(_ context.Context, call serviceCall) error {
		if call.method == "review" {
			reviewedBy = call.principal
		}
		if call.method == "confirm" && call.principal != reviewedBy {
			return actressnamesync.ErrPrincipal
		}
		return nil
	}
	require.Equal(t, 201, f.serve(newRequest("POST", "/reviews", reviewBody)).Code)
	r := newRequest("POST", "/jobs", confirmBody)
	r.Header.Set("Authorization", "Bearer "+token.TokenPrefix+"bob")
	require.Equal(t, 403, f.serve(r).Code)
	r = newRequest("POST", "/jobs", confirmBody)
	r.Header.Del("Authorization")
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-alice"})
	require.Equal(t, 403, f.serve(r).Code, "a token review is not a session review even for one person")
	require.Equal(t, 202, f.serve(newRequest("POST", "/jobs", confirmBody)).Code)
}

func TestWriteIntentAndOriginEnforcedOnEveryWrite(t *testing.T) {
	for _, tc := range []struct{ path, body string }{{"/reviews", reviewBody}, {"/jobs", confirmBody}, {"/jobs/" + testJobID + "/cancel", cancelBody}} {
		for _, attack := range []struct {
			name   string
			mutate func(*http.Request)
			status int
		}{
			{"missing intent", func(r *http.Request) { r.Header.Del(intentHeader) }, 403},
			{"ordinary xhr intent", func(r *http.Request) { r.Header.Set(intentHeader, "XMLHttpRequest") }, 403},
			{"duplicate intent", func(r *http.Request) { r.Header.Add(intentHeader, intentValue) }, 403},
			{"foreign origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
			{"null origin despite configured null", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
			{"wildcard does not allow", func(r *http.Request) { r.Header.Set("Origin", "https://arbitrary.example") }, 403},
			{"multiple origins", func(r *http.Request) {
				r.Header.Add("Origin", "http://api.example")
				r.Header.Add("Origin", "https://evil.example")
			}, 403},
			{"origin user info", func(r *http.Request) { r.Header.Set("Origin", "http://evil@api.example") }, 403},
			{"origin path", func(r *http.Request) { r.Header.Set("Origin", "http://api.example/") }, 403},
			{"origin query", func(r *http.Request) { r.Header.Set("Origin", "http://api.example?secret=x") }, 403},
			{"origin fragment", func(r *http.Request) { r.Header.Set("Origin", "http://api.example#") }, 403},
			{"origin invalid port", func(r *http.Request) { r.Header.Set("Origin", "http://api.example:bad") }, 403},
			{"origin prefix spoof", func(r *http.Request) { r.Header.Set("Origin", "http://api.example.evil") }, 403},
			{"different scheme", func(r *http.Request) { r.Header.Set("Origin", "https://api.example") }, 403},
			{"forwarded proto spoof", func(r *http.Request) {
				r.Header.Set("Origin", "https://api.example")
				r.Header.Set("X-Forwarded-Proto", "https")
			}, 403},
			{"forwarded host spoof", func(r *http.Request) {
				r.Header.Set("Origin", "https://evil.example")
				r.Header.Set("X-Forwarded-Host", "evil.example")
				r.Header.Set("X-Forwarded-Proto", "https")
			}, 403},
			{"contradictory fetch site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
			{"missing type", func(r *http.Request) { r.Header.Del("Content-Type") }, 415},
			{"simple text", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
			{"form", func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, 415},
			{"multipart", func(r *http.Request) { r.Header.Set("Content-Type", "multipart/form-data; boundary=abc") }, 415},
		} {
			t.Run(tc.path+attack.name, func(t *testing.T) {
				f := newFixture(t)
				r := newRequest("POST", tc.path, tc.body)
				r.Header.Del("Authorization")
				r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-alice"})
				attack.mutate(r)
				w := f.serve(r)
				require.Equal(t, attack.status, w.Code, w.Body.String())
				require.Empty(t, f.service.recorded())
			})
		}
	}
}

func TestAllowedWriteOriginsAndCLI(t *testing.T) {
	for _, tc := range []struct {
		name, origin string
		tls          bool
	}{
		{"cli", "", false}, {"same", "http://api.example", false}, {"same default port", "http://api.example:80", false}, {"tls same", "https://api.example", true}, {"tls default port", "https://api.example:443", true}, {"configured cross origin", "https://trusted.example", false}, {"configured desktop", "wails://wails.localhost", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			r := newRequest("POST", "/reviews", reviewBody)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			r.Header.Set("Content-Type", "application/json; charset=utf-8")
			w := f.serve(r)
			require.Equal(t, 201, w.Code, w.Body.String())
		})
	}
}

func TestRegisterRoutesNilRuntimeAndMissingServiceFailClosed(t *testing.T) {
	for _, nilRuntime := range []bool{true, false} {
		t.Run(map[bool]string{true: "nil runtime", false: "nil service"}[nilRuntime], func(t *testing.T) {
			router := gin.New()
			deps := &core.APIDeps{Auth: authStub{}}
			group := router.Group("/api/v1", auth.RequireTokenOrSession(deps))
			var rt *core.APIRuntime
			if !nilRuntime {
				rt = core.NewAPIRuntime(deps)
				t.Cleanup(rt.Shutdown)
			}
			RegisterRoutes(group, group, rt)
			r := newRequest("GET", "/status", "")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			require.Equal(t, 503, w.Code, w.Body.String())
		})
	}
}
