package genre

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeGenreConfigStore struct {
	mu        sync.Mutex
	ignored   []string
	favorites []string
	err       error
	setErr    error
}

func (f *fakeGenreConfigStore) GetIgnoreGenres(context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.ignored...), nil
}

func (f *fakeGenreConfigStore) SetIgnoreGenres(_ context.Context, genres []string) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.err != nil {
		return f.err
	}
	f.ignored = append([]string(nil), genres...)
	return nil
}

func (f *fakeGenreConfigStore) GetFavoriteGenres(context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.favorites...), nil
}

func (f *fakeGenreConfigStore) SetFavoriteGenres(_ context.Context, genres []string) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.err != nil {
		return f.err
	}
	f.favorites = append([]string(nil), genres...)
	return nil
}

func (f *fakeGenreConfigStore) AddIgnoreGenre(_ context.Context, genre string) ([]string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, false, f.err
	}
	if containsString(f.ignored, genre) {
		return cloneStrings(f.ignored), false, nil
	}
	if f.setErr != nil {
		return nil, false, f.setErr
	}
	f.ignored = append(cloneStrings(f.ignored), genre)
	return cloneStrings(f.ignored), true, nil
}

func (f *fakeGenreConfigStore) RemoveIgnoreGenre(_ context.Context, genre string) ([]string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, false, f.err
	}
	if !containsString(f.ignored, genre) {
		return cloneStrings(f.ignored), false, nil
	}
	if f.setErr != nil {
		return nil, false, f.setErr
	}
	f.ignored = removeString(f.ignored, genre)
	return cloneStrings(f.ignored), true, nil
}

func (f *fakeGenreConfigStore) AddFavoriteGenre(_ context.Context, genre string) ([]string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, false, f.err
	}
	if containsString(f.favorites, genre) {
		return cloneStrings(f.favorites), false, nil
	}
	if f.setErr != nil {
		return nil, false, f.setErr
	}
	f.favorites = append(cloneStrings(f.favorites), genre)
	return cloneStrings(f.favorites), true, nil
}

func (f *fakeGenreConfigStore) RemoveFavoriteGenre(_ context.Context, genre string) ([]string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, false, f.err
	}
	if !containsString(f.favorites, genre) {
		return cloneStrings(f.favorites), false, nil
	}
	if f.setErr != nil {
		return nil, false, f.setErr
	}
	f.favorites = removeString(f.favorites, genre)
	return cloneStrings(f.favorites), true, nil
}

func newListTestDeps(store GenreConfigStore) GenreDeps {
	deps := NewGenreDeps(database.ReplacementRepos{}, database.TranslationRepos{})
	deps.ConfigStore = store
	return deps
}

func setupListRouter(deps GenreDeps) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	ignored := protected.Group("/genres/ignored")
	ignored.GET("", listIgnoredGenres(deps))
	ignored.POST("", addIgnoredGenre(deps))
	ignored.PUT("", replaceIgnoredGenres(deps))
	ignored.DELETE("", deleteIgnoredGenre(deps))
	favorites := protected.Group("/genres/favorites")
	favorites.GET("", listFavoriteGenres(deps))
	favorites.POST("", addFavoriteGenre(deps))
	favorites.PUT("", replaceFavoriteGenres(deps))
	favorites.DELETE("", deleteFavoriteGenre(deps))
	return router
}

func doJSON(router *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestIgnoredGenres_List(t *testing.T) {
	store := &fakeGenreConfigStore{ignored: []string{"Sample", "Trailer"}}
	router := setupListRouter(newListTestDeps(store))

	w := doJSON(router, "GET", "/genres/ignored", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var resp ignoredGenresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{"Sample", "Trailer"}, resp.IgnoredGenres)
	assert.Equal(t, 2, resp.Count)
}

func TestIgnoredGenres_ListEmpty(t *testing.T) {
	router := setupListRouter(newListTestDeps(&fakeGenreConfigStore{}))
	w := doJSON(router, "GET", "/genres/ignored", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var resp ignoredGenresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.IgnoredGenres)
	assert.Equal(t, 0, resp.Count)
}

func TestIgnoredGenres_Add(t *testing.T) {
	store := &fakeGenreConfigStore{ignored: []string{"Sample"}}
	router := setupListRouter(newListTestDeps(store))

	w := doJSON(router, "POST", "/genres/ignored", genreAddRequest{Genre: "Trailer"})
	require.Equal(t, http.StatusCreated, w.Code)

	var resp ignoredGenresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{"Sample", "Trailer"}, resp.IgnoredGenres)
	assert.Equal(t, []string{"Sample", "Trailer"}, store.ignored)
}

func TestIgnoredGenres_AddIdempotent(t *testing.T) {
	store := &fakeGenreConfigStore{ignored: []string{"Sample"}}
	router := setupListRouter(newListTestDeps(store))

	w := doJSON(router, "POST", "/genres/ignored", genreAddRequest{Genre: "Sample"})
	require.Equal(t, http.StatusOK, w.Code)

	var resp ignoredGenresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.IgnoredGenres, 1)
	assert.Equal(t, "Sample", resp.IgnoredGenres[0])
}

func TestIgnoredGenres_AddEmpty(t *testing.T) {
	router := setupListRouter(newListTestDeps(&fakeGenreConfigStore{}))
	w := doJSON(router, "POST", "/genres/ignored", genreAddRequest{Genre: "  "})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIgnoredGenres_AddTrimsWhitespace(t *testing.T) {
	store := &fakeGenreConfigStore{}
	router := setupListRouter(newListTestDeps(store))

	w := doJSON(router, "POST", "/genres/ignored", genreAddRequest{Genre: "  VR  "})
	require.Equal(t, http.StatusCreated, w.Code)

	assert.Equal(t, []string{"VR"}, store.ignored)
}

func TestIgnoredGenres_Delete(t *testing.T) {
	store := &fakeGenreConfigStore{ignored: []string{"Sample", "Trailer"}}
	router := setupListRouter(newListTestDeps(store))

	w := doJSON(router, "DELETE", "/genres/ignored?genre=Trailer", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var resp ignoredGenresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{"Sample"}, resp.IgnoredGenres)
	assert.Equal(t, []string{"Sample"}, store.ignored)
}

func TestIgnoredGenres_DeleteNotFound(t *testing.T) {
	router := setupListRouter(newListTestDeps(&fakeGenreConfigStore{ignored: []string{"Sample"}}))
	w := doJSON(router, "DELETE", "/genres/ignored?genre=Missing", nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestIgnoredGenres_DeleteMissingParam(t *testing.T) {
	router := setupListRouter(newListTestDeps(&fakeGenreConfigStore{}))
	w := doJSON(router, "DELETE", "/genres/ignored", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIgnoredGenres_Replace(t *testing.T) {
	store := &fakeGenreConfigStore{ignored: []string{"Old"}}
	router := setupListRouter(newListTestDeps(store))

	w := doJSON(router, "PUT", "/genres/ignored", genreListUpdateRequest{Genres: []string{"Sample", "Trailer"}})
	require.Equal(t, http.StatusOK, w.Code)

	var resp ignoredGenresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{"Sample", "Trailer"}, resp.IgnoredGenres)
	assert.Equal(t, []string{"Sample", "Trailer"}, store.ignored)
}

func TestIgnoredGenres_ReplaceDedupesAndTrims(t *testing.T) {
	store := &fakeGenreConfigStore{}
	router := setupListRouter(newListTestDeps(store))

	w := doJSON(router, "PUT", "/genres/ignored", genreListUpdateRequest{Genres: []string{"  VR ", "HD", "VR", ""}})
	require.Equal(t, http.StatusOK, w.Code)

	var resp ignoredGenresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{"VR", "HD"}, resp.IgnoredGenres)
	assert.Equal(t, []string{"VR", "HD"}, store.ignored)
}

func TestIgnoredGenres_StoreError(t *testing.T) {
	store := &fakeGenreConfigStore{err: fmt.Errorf("disk full")}
	router := setupListRouter(newListTestDeps(store))

	w := doJSON(router, "GET", "/genres/ignored", nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestFavorites_AddDeleteReplace(t *testing.T) {
	store := &fakeGenreConfigStore{}
	router := setupListRouter(newListTestDeps(store))

	w := doJSON(router, "POST", "/genres/favorites", genreAddRequest{Genre: "Drama"})
	require.Equal(t, http.StatusCreated, w.Code)

	w = doJSON(router, "POST", "/genres/favorites", genreAddRequest{Genre: "Action"})
	require.Equal(t, http.StatusCreated, w.Code)

	w = doJSON(router, "GET", "/genres/favorites", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var resp favoriteGenresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{"Drama", "Action"}, resp.Favorites)

	w = doJSON(router, "PUT", "/genres/favorites", genreListUpdateRequest{Genres: []string{"Comedy"}})
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{"Comedy"}, resp.Favorites)

	w = doJSON(router, "DELETE", "/genres/favorites?genre=Comedy", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Favorites)
}

func TestFavorites_AddIdempotent(t *testing.T) {
	store := &fakeGenreConfigStore{favorites: []string{"Drama"}}
	router := setupListRouter(newListTestDeps(store))

	w := doJSON(router, "POST", "/genres/favorites", genreAddRequest{Genre: "Drama"})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"Drama"}, store.favorites)
}

func TestConfigStoreNotConfigured(t *testing.T) {
	deps := NewGenreDeps(database.ReplacementRepos{}, database.TranslationRepos{})
	deps.ConfigStore = nil
	router := setupListRouter(deps)

	w := doJSON(router, "GET", "/genres/ignored", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}
