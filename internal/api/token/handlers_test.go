package token

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRegisterRoutes(t *testing.T) {
	repo, _ := setupHandlerTestDB(t)
	svc := NewTokenService(repo)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("/api/v1")
	writeProtected := router.Group("/api/v1")

	RegisterRoutes(protected, writeProtected, svc)

	routes := router.Routes()
	routeMap := make(map[string]bool)
	for _, r := range routes {
		routeMap[r.Method+":"+r.Path] = true
	}

	assert.True(t, routeMap["GET:/api/v1/tokens"], "GET /api/v1/tokens route should be registered")
	assert.True(t, routeMap["POST:/api/v1/tokens"], "POST /api/v1/tokens route should be registered")
	assert.True(t, routeMap["DELETE:/api/v1/tokens/:id"], "DELETE /api/v1/tokens/:id route should be registered")
	assert.True(t, routeMap["POST:/api/v1/tokens/:id/regenerate"], "POST /api/v1/tokens/:id/regenerate route should be registered")
}

func setupHandlerTestDB(t *testing.T) (*database.ApiTokenRepository, *gorm.DB) {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := &database.DB{DB: gormDB}
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := database.NewApiTokenRepository(db)
	return repo, gormDB
}

func setupHandlerRouter(repo *database.ApiTokenRepository) *gin.Engine {
	gin.SetMode(gin.TestMode)

	svc := NewTokenService(repo)

	router := gin.New()
	api := router.Group("/api/v1")

	api.POST("/tokens", createToken(svc))
	api.GET("/tokens", listTokens(svc))
	api.DELETE("/tokens/:id", revokeToken(svc))
	api.POST("/tokens/:id/regenerate", regenerateToken(svc))

	return router
}

func Test_createToken(t *testing.T) {
	repo, _ := setupHandlerTestDB(t)
	router := setupHandlerRouter(repo)

	t.Run("returns token in response", func(t *testing.T) {
		body, _ := json.Marshal(createTokenRequest{Name: "handler-test"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var resp tokenResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "handler-test", resp.Name)
		assert.True(t, len(resp.Token) > 0)
		assert.True(t, len(resp.ID) > 0)
		assert.Equal(t, "jv_", resp.Token[:3])
	})

	t.Run("name stored correctly", func(t *testing.T) {
		body, _ := json.Marshal(createTokenRequest{Name: "named-token"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var resp tokenResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "named-token", resp.Name)
	})

	t.Run("bad request on invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("create with empty name", func(t *testing.T) {
		body, _ := json.Marshal(createTokenRequest{Name: ""})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
	})
}

func Test_listTokens(t *testing.T) {
	t.Run("returns list of active tokens", func(t *testing.T) {
		repo, _ := setupHandlerTestDB(t)
		svc := NewTokenService(repo)
		svc.Create(context.Background(), "list-a")
		svc.Create(context.Background(), "list-b")
		router := setupHandlerRouter(repo)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tokens", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp tokenListResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 2, resp.Count)
		assert.Len(t, resp.Tokens, 2)
	})

	t.Run("list empty returns empty array", func(t *testing.T) {
		repo, _ := setupHandlerTestDB(t)
		router := setupHandlerRouter(repo)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tokens", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp tokenListResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 0, resp.Count)
		assert.Empty(t, resp.Tokens)
	})
}

func Test_revokeToken(t *testing.T) {
	repo, _ := setupHandlerTestDB(t)
	svc := NewTokenService(repo)
	apiToken, _, err := svc.Create(context.Background(), "revoke-handler")
	require.NoError(t, err)

	router := setupHandlerRouter(repo)

	t.Run("returns 200 and token revoked", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tokens/"+apiToken.ID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "token revoked", resp["message"])

		found, err := repo.FindByID(context.Background(), apiToken.ID)
		require.NoError(t, err)
		assert.NotNil(t, found.RevokedAt)
	})

	t.Run("not found for nonexistent id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tokens/nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func Test_regenerateToken(t *testing.T) {
	repo, _ := setupHandlerTestDB(t)
	svc := NewTokenService(repo)
	apiToken, oldFullToken, err := svc.Create(context.Background(), "regen-handler")
	require.NoError(t, err)

	router := setupHandlerRouter(repo)

	t.Run("returns new token and old invalid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens/"+apiToken.ID+"/regenerate", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp tokenResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, apiToken.ID, resp.ID)
		assert.NotEqual(t, oldFullToken, resp.Token)
		assert.True(t, len(resp.Token) > 0)

		_, err := repo.FindByTokenHash(context.Background(), HashToken(oldFullToken))
		assert.Error(t, err, "old token hash should not be found after regenerate")
	})

	t.Run("not found for nonexistent id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens/nonexistent/regenerate", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func Test_createToken_InternalError(t *testing.T) {
	gormDB, _ := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	db := &database.DB{DB: gormDB}
	_ = db.Close()

	svc := NewTokenService(database.NewApiTokenRepository(db))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api/v1")
	api.POST("/tokens", createToken(svc))

	body, _ := json.Marshal(createTokenRequest{Name: "fail-test"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func Test_revokeToken_InternalError(t *testing.T) {
	gormDB, _ := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	db := &database.DB{DB: gormDB}
	_ = db.Close()

	svc := NewTokenService(database.NewApiTokenRepository(db))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api/v1")
	api.DELETE("/tokens/:id", revokeToken(svc))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tokens/some-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func Test_regenerateToken_InternalError(t *testing.T) {
	gormDB, _ := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	db := &database.DB{DB: gormDB}
	_ = db.Close()

	svc := NewTokenService(database.NewApiTokenRepository(db))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api/v1")
	api.POST("/tokens/:id/regenerate", regenerateToken(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens/some-id/regenerate", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
