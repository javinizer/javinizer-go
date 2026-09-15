package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
)

type initializedSessionErrorAuth struct{}

func (initializedSessionErrorAuth) SessionTTL() time.Duration           { return time.Hour }
func (initializedSessionErrorAuth) PersistentSessionTTL() time.Duration { return 30 * 24 * time.Hour }
func (initializedSessionErrorAuth) IsInitialized() bool                 { return true }
func (initializedSessionErrorAuth) AuthenticateSession(string) (string, error) {
	return "", ErrAuthNotInitialized
}
func (initializedSessionErrorAuth) Setup(string, string) error                 { return nil }
func (initializedSessionErrorAuth) Login(string, string, bool) (string, error) { return "", nil }
func (initializedSessionErrorAuth) Logout(string)                              {}
func (initializedSessionErrorAuth) ValidateToken(context.Context, string) (string, error) {
	return "", nil
}
func (initializedSessionErrorAuth) UpdateTokenLastUsed(context.Context, string) error { return nil }
func (initializedSessionErrorAuth) GetEnv(key string) string                          { return os.Getenv(key) }

func TestSessionMiddlewareHandlesLateUninitializedAuthPR260(t *testing.T) {
	for _, handler := range []struct {
		name       string
		middleware func(*core.APIRuntime) gin.HandlerFunc
	}{
		{name: "authenticated", middleware: requireAuthenticated},
		{name: "token or session", middleware: requireTokenOrSession},
	} {
		t.Run(handler.name, func(t *testing.T) {
			cfg := config.DefaultConfig(nil, nil)
			deps := createTestDeps(t, cfg, filepath.Join(t.TempDir(), "config.yaml"))
			deps.Auth = initializedSessionErrorAuth{}
			called := false
			router := gin.New()
			router.GET("/test", handler.middleware(testkit.GetTestRuntime(deps)), func(*gin.Context) { called = true })
			request := httptest.NewRequest(http.MethodGet, "/test", nil)
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, http.StatusServiceUnavailable, response.Code)
			assert.False(t, called)
		})
	}
}
