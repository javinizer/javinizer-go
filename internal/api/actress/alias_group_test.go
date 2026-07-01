package actress

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	mocks "github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGetAliasGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, aliasRepo := setupActressAliasTestDB(t)

	// Seed: 新セリナ is canonical; 青木桃/朝日芹奈/堤セリナ are her aliases.
	seed := []models.ActressAlias{
		{AliasName: "青木桃", CanonicalName: "新セリナ"},
		{AliasName: "朝日芹奈", CanonicalName: "新セリナ"},
		{AliasName: "堤セリナ", CanonicalName: "新セリナ"},
		{AliasName: "与田さくら", CanonicalName: "尾崎えりか"},
	}
	for _, a := range seed {
		require.NoError(t, aliasRepo.Create(context.Background(), &a))
	}

	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressAliasRepo: aliasRepo}}
	router := gin.New()
	router.GET("/actresses/alias-group", getAliasGroup(deps))

	doGet := func(name string) (int, aliasGroupResponse) {
		path := "/actresses/alias-group"
		if name != "" {
			path += "?name=" + url.QueryEscape(name)
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var resp aliasGroupResponse
		if w.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		}
		return w.Code, resp
	}

	t.Run("alias input resolves to full group", func(t *testing.T) {
		code, resp := doGet("朝日芹奈")
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "新セリナ", resp.Canonical)
		// Canonical first, then all aliases, deduplicated.
		require.Len(t, resp.Names, 4)
		assert.Equal(t, "新セリナ", resp.Names[0])
		assert.Contains(t, resp.Names, "青木桃")
		assert.Contains(t, resp.Names, "朝日芹奈")
		assert.Contains(t, resp.Names, "堤セリナ")
	})

	t.Run("canonical input resolves to full group", func(t *testing.T) {
		code, resp := doGet("新セリナ")
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "新セリナ", resp.Canonical)
		require.Len(t, resp.Names, 4)
		assert.Equal(t, "新セリナ", resp.Names[0])
	})

	t.Run("unknown name returns empty group", func(t *testing.T) {
		code, resp := doGet("弥生みづき")
		require.Equal(t, http.StatusOK, code)
		assert.Empty(t, resp.Canonical)
		assert.Empty(t, resp.Names)
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/actresses/alias-group", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("whitespace-only name returns 400", func(t *testing.T) {
		code, _ := doGet("   ")
		assert.Equal(t, http.StatusBadRequest, code)
	})

	// Ensure DB is closed via t.Cleanup registered by setup helper.
	_ = db
}

// setupActressAliasTestDB creates an in-memory DB with migrations and returns
// the alias repository. The DB is closed via t.Cleanup.
func setupActressAliasTestDB(t *testing.T) (*database.DB, database.ActressAliasRepositoryInterface) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	return db, db.Repositories().ActressAliasRepo
}

func TestGetAliasGroup_NilRepo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// ActressDeps with no alias repository wired — handler must short-circuit
	// with an empty 200 response rather than nil-deref.
	deps := ActressDeps{}
	router := gin.New()
	router.GET("/actresses/alias-group", getAliasGroup(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses/alias-group?name=x", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp aliasGroupResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Canonical)
	assert.Empty(t, resp.Names)
}

func TestGetAliasGroup_RepoError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := mocks.NewMockActressAliasRepositoryInterface(t)
	repo.EXPECT().GetAliasGroup(mock.Anything, "朝日芹奈").
		Return(database.AliasGroup{}, errors.New("db unavailable"))
	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressAliasRepo: repo}}
	router := gin.New()
	router.GET("/actresses/alias-group", getAliasGroup(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses/alias-group?name="+url.QueryEscape("朝日芹奈"), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
