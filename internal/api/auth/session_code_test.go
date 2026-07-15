package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errAlreadySetAuth is a commandutil.AuthProvider whose IsInitialized reports
// false (so setupAuth's pre-check passes) but Setup returns ErrAuthAlreadySet.
// This exercises the dedicated ErrAuthAlreadySet branch of the setup error
// switch (Code: AUTH_USER_EXISTS), which a real *AuthManager cannot reach
// because IsInitialized flips to true before Setup can return that error.
type errAlreadySetAuth struct{}

func (errAlreadySetAuth) SessionTTL() time.Duration           { return time.Hour }
func (errAlreadySetAuth) PersistentSessionTTL() time.Duration { return 30 * 24 * time.Hour }
func (errAlreadySetAuth) IsInitialized() bool                 { return false }
func (errAlreadySetAuth) AuthenticateSession(string) (string, error) {
	return "", ErrAuthNotInitialized
}
func (errAlreadySetAuth) Setup(string, string) error { return ErrAuthAlreadySet }
func (errAlreadySetAuth) Login(string, string, bool) (string, error) {
	return "", ErrAuthNotInitialized
}
func (errAlreadySetAuth) Logout(string) {}
func (errAlreadySetAuth) ValidateToken(context.Context, string) (string, error) {
	return "", ErrAuthNotInitialized
}
func (errAlreadySetAuth) UpdateTokenLastUsed(context.Context, string) error { return nil }
func (errAlreadySetAuth) GetEnv(key string) string                          { return os.Getenv(key) }

// TestSetupAuth_ErrAuthAlreadySet_Returns409WithCode covers the i18n error-code
// branch added in PR #144: when deps.Auth.Setup returns ErrAuthAlreadySet (and
// IsInitialized is still false), the handler must respond 409 with
// Code: "AUTH_USER_EXISTS".
func TestSetupAuth_ErrAuthAlreadySet_Returns409WithCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JAVINIZER_SETUP_SECRET", "test-secret")
	cfg := config.DefaultConfig(nil, nil)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, cfg, configFile)
	deps.Auth = errAlreadySetAuth{}

	router := gin.New()
	router.POST("/setup", setupAuth(testkit.GetTestRuntime(deps)))

	body := []byte(`{"username":"admin","password":"password123"}`)
	req := httptest.NewRequest("POST", "/setup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Setup-Secret", "test-secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "AUTH_USER_EXISTS", resp.Code)
	assert.Equal(t, "authentication is already initialized", resp.Error)
}
