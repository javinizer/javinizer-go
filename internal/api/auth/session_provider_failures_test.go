package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

type failingSessionProvider struct {
	commandutil.AuthProvider
	initialized bool
	setupErr    error
	loginErr    error
	loginCalls  int
}

func (p *failingSessionProvider) IsInitialized() bool        { return p.initialized }
func (p *failingSessionProvider) GetEnv(string) string       { return "" }
func (p *failingSessionProvider) Setup(string, string) error { return p.setupErr }
func (p *failingSessionProvider) Login(string, string, bool) (string, error) {
	p.loginCalls++
	return "", p.loginErr
}

func TestSessionProviderFailuresReturnSafeResponses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		setup      bool
		provider   failingSessionProvider
		status     int
		message    string
		loginCalls int
	}{
		{name: "setup storage failure", setup: true, provider: failingSessionProvider{setupErr: errors.New("private backend failure")}, status: http.StatusInternalServerError, message: "failed to initialize authentication"},
		{name: "concurrent setup wins", setup: true, provider: failingSessionProvider{setupErr: ErrAuthAlreadySet}, status: http.StatusConflict, message: "authentication is already initialized"},
		{name: "session creation fails after setup", setup: true, provider: failingSessionProvider{loginErr: errors.New("private backend failure")}, status: http.StatusInternalServerError, message: "failed to create authenticated session", loginCalls: 1},
		{name: "login storage error", provider: failingSessionProvider{initialized: true, loginErr: errors.New("private backend failure")}, status: http.StatusInternalServerError, message: "authentication failed", loginCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
			deps.Auth = &tc.provider
			rt := core.NewAPIRuntime(deps)
			router := gin.New()
			handler := loginAuth(rt)
			if tc.setup {
				handler = setupAuth(rt)
			}
			router.POST("/auth", handler)
			req := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(`{"username":"admin","password":"test password"}`))
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "127.0.0.1:1234"
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
			require.Contains(t, rec.Body.String(), tc.message)
			require.NotContains(t, rec.Body.String(), "private backend failure")
			require.Empty(t, rec.Result().Cookies())
			require.Equal(t, tc.loginCalls, tc.provider.loginCalls)
		})
	}
}
