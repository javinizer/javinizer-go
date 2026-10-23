package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/token"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type failingTokenUsageRepo struct {
	database.ApiTokenRepositoryInterface
	revoked   bool
	seenHash  string
	updatedID string
}

func (r *failingTokenUsageRepo) FindByTokenHash(_ context.Context, hash string) (*models.ApiToken, error) {
	r.seenHash = hash
	result := &models.ApiToken{ID: "token-one"}
	if r.revoked {
		now := time.Now()
		result.RevokedAt = &now
	}
	return result, nil
}
func (r *failingTokenUsageRepo) UpdateLastUsed(_ context.Context, id string) error {
	r.updatedID = id
	return errors.New("usage storage unavailable")
}

func TestBearerTokenUsageUpdateFailureDoesNotRejectAuthentication(t *testing.T) {
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
	repo := &failingTokenUsageRepo{}
	manager := &AuthManager{credentials: &storedCredentials{Username: "admin"}}
	manager.SetApiTokenRepo(repo)
	deps.Auth = manager
	router := gin.New()
	router.GET("/protected", requireTokenOrSession(core.NewAPIRuntime(deps)), func(c *gin.Context) {
		require.Equal(t, "token", c.GetString("auth_method"))
		require.Equal(t, "token-one", c.GetString("token_id"))
		c.Status(http.StatusNoContent)
	})
	raw := token.TokenPrefix + "test-token"
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, token.HashToken(raw), repo.seenHash)
	require.Equal(t, "token-one", repo.updatedID)
}
func TestValidateTokenRejectsRevokedRepositoryResult(t *testing.T) {
	manager := &AuthManager{}
	manager.SetApiTokenRepo(&failingTokenUsageRepo{revoked: true})
	id, err := manager.ValidateToken(context.Background(), "hash")
	require.Empty(t, id)
	require.ErrorContains(t, err, "token revoked")
}
func TestAuthManagerGetEnvFallback(t *testing.T) {
	t.Setenv("JAVINIZER_TEST_AUTH_ENV", "fallback")
	require.Equal(t, "fallback", (&AuthManager{}).GetEnv("JAVINIZER_TEST_AUTH_ENV"))
}
