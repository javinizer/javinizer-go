package genre

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupNilStoreRouter() *gin.Engine {
	return setupListRouter(newListTestDeps(nil))
}

func setupStoreErrRouter() *gin.Engine {
	return setupListRouter(newListTestDeps(&fakeGenreConfigStore{err: fmt.Errorf("disk full")}))
}

func doRaw(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestIgnoredGenres_Replace_NotConfigured(t *testing.T) {
	w := doJSON(setupNilStoreRouter(), "PUT", "/genres/ignored", genreListUpdateRequest{Genres: []string{"VR"}})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestIgnoredGenres_Replace_BindError(t *testing.T) {
	w := doRaw(setupListRouter(newListTestDeps(&fakeGenreConfigStore{})), "PUT", "/genres/ignored", "{bad json")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIgnoredGenres_Replace_StoreError(t *testing.T) {
	w := doJSON(setupStoreErrRouter(), "PUT", "/genres/ignored", genreListUpdateRequest{Genres: []string{"VR"}})
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIgnoredGenres_Add_NotConfigured(t *testing.T) {
	w := doJSON(setupNilStoreRouter(), "POST", "/genres/ignored", genreAddRequest{Genre: "VR"})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestIgnoredGenres_Add_BindError(t *testing.T) {
	w := doRaw(setupListRouter(newListTestDeps(&fakeGenreConfigStore{})), "POST", "/genres/ignored", "{bad json")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIgnoredGenres_Add_GetStoreError(t *testing.T) {
	w := doJSON(setupStoreErrRouter(), "POST", "/genres/ignored", genreAddRequest{Genre: "VR"})
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIgnoredGenres_Add_SetStoreError(t *testing.T) {
	store := &fakeGenreConfigStore{ignored: []string{"Sample"}, setErr: fmt.Errorf("disk full")}
	router := setupListRouter(newListTestDeps(store))
	w := doJSON(router, "POST", "/genres/ignored", genreAddRequest{Genre: "VR"})
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIgnoredGenres_Delete_NotConfigured(t *testing.T) {
	w := doJSON(setupNilStoreRouter(), "DELETE", "/genres/ignored?genre=VR", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestIgnoredGenres_Delete_GetStoreError(t *testing.T) {
	w := doJSON(setupStoreErrRouter(), "DELETE", "/genres/ignored?genre=VR", nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIgnoredGenres_Delete_SetStoreError(t *testing.T) {
	store := &fakeGenreConfigStore{ignored: []string{"VR"}, setErr: fmt.Errorf("disk full")}
	router := setupListRouter(newListTestDeps(store))
	w := doJSON(router, "DELETE", "/genres/ignored?genre=VR", nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestFavorites_List_NotConfigured(t *testing.T) {
	w := doJSON(setupNilStoreRouter(), "GET", "/genres/favorites", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestFavorites_List_StoreError(t *testing.T) {
	w := doJSON(setupStoreErrRouter(), "GET", "/genres/favorites", nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestFavorites_Replace_NotConfigured(t *testing.T) {
	w := doJSON(setupNilStoreRouter(), "PUT", "/genres/favorites", genreListUpdateRequest{Genres: []string{"VR"}})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestFavorites_Replace_BindError(t *testing.T) {
	w := doRaw(setupListRouter(newListTestDeps(&fakeGenreConfigStore{})), "PUT", "/genres/favorites", "{bad json")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestFavorites_Replace_StoreError(t *testing.T) {
	w := doJSON(setupStoreErrRouter(), "PUT", "/genres/favorites", genreListUpdateRequest{Genres: []string{"VR"}})
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestFavorites_Add_NotConfigured(t *testing.T) {
	w := doJSON(setupNilStoreRouter(), "POST", "/genres/favorites", genreAddRequest{Genre: "VR"})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestFavorites_Add_BindError(t *testing.T) {
	w := doRaw(setupListRouter(newListTestDeps(&fakeGenreConfigStore{})), "POST", "/genres/favorites", "{bad json")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestFavorites_Add_Empty(t *testing.T) {
	w := doJSON(setupListRouter(newListTestDeps(&fakeGenreConfigStore{})), "POST", "/genres/favorites", genreAddRequest{Genre: "  "})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestFavorites_Add_GetStoreError(t *testing.T) {
	w := doJSON(setupStoreErrRouter(), "POST", "/genres/favorites", genreAddRequest{Genre: "VR"})
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestFavorites_Add_SetStoreError(t *testing.T) {
	store := &fakeGenreConfigStore{favorites: []string{"Drama"}, setErr: fmt.Errorf("disk full")}
	router := setupListRouter(newListTestDeps(store))
	w := doJSON(router, "POST", "/genres/favorites", genreAddRequest{Genre: "VR"})
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestFavorites_Delete_NotConfigured(t *testing.T) {
	w := doJSON(setupNilStoreRouter(), "DELETE", "/genres/favorites?genre=VR", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestFavorites_Delete_MissingParam(t *testing.T) {
	w := doJSON(setupListRouter(newListTestDeps(&fakeGenreConfigStore{})), "DELETE", "/genres/favorites", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestFavorites_Delete_NotFound(t *testing.T) {
	router := setupListRouter(newListTestDeps(&fakeGenreConfigStore{favorites: []string{"Drama"}}))
	w := doJSON(router, "DELETE", "/genres/favorites?genre=Missing", nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestFavorites_Delete_GetStoreError(t *testing.T) {
	w := doJSON(setupStoreErrRouter(), "DELETE", "/genres/favorites?genre=VR", nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestFavorites_Delete_SetStoreError(t *testing.T) {
	store := &fakeGenreConfigStore{favorites: []string{"VR"}, setErr: fmt.Errorf("disk full")}
	router := setupListRouter(newListTestDeps(store))
	w := doJSON(router, "DELETE", "/genres/favorites?genre=VR", nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

// noop store writes return ErrGenreConfigStoreNotConfigured, which handlers
// must map to 503 (not 500), matching the explicit nil-store guard + swagger.
func TestNoopStore_IgnoredAddReturns503(t *testing.T) {
	router := setupListRouter(newListTestDeps(noopGenreConfigStore{}))
	w := doJSON(router, "POST", "/genres/ignored", genreAddRequest{Genre: "VR"})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestNoopStore_IgnoredReplaceReturns503(t *testing.T) {
	router := setupListRouter(newListTestDeps(noopGenreConfigStore{}))
	w := doJSON(router, "PUT", "/genres/ignored", genreListUpdateRequest{Genres: []string{"VR"}})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestNoopStore_FavoritesAddReturns503(t *testing.T) {
	router := setupListRouter(newListTestDeps(noopGenreConfigStore{}))
	w := doJSON(router, "POST", "/genres/favorites", genreAddRequest{Genre: "VR"})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestNoopStore_FavoritesReplaceReturns503(t *testing.T) {
	router := setupListRouter(newListTestDeps(noopGenreConfigStore{}))
	w := doJSON(router, "PUT", "/genres/favorites", genreListUpdateRequest{Genres: []string{"VR"}})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}
