package genre

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

type ignoredGenresResponse struct {
	IgnoredGenres []string `json:"ignored_genres"`
	Count         int      `json:"count"`
}

type favoriteGenresResponse struct {
	Favorites []string `json:"favorites"`
	Count     int      `json:"count"`
}

type genreListUpdateRequest struct {
	Genres []string `json:"genres"`
}

type genreAddRequest struct {
	Genre string `json:"genre"`
}

// listIgnoredGenres godoc
// @Summary List ignored genres
// @Description Get the configured ignore_genres list (genres excluded from scraping/processing)
// @Tags genres
// @Produce json
// @Success 200 {object} ignoredGenresResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/genres/ignored [get]
func listIgnoredGenres(deps GenreDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.ConfigStore == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "genre config store is not configured"})
			return
		}
		genres, err := deps.ConfigStore.GetIgnoreGenres(c.Request.Context())
		if err != nil {
			c.JSON(storeErrorStatus(err), contracts.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, ignoredGenresResponse{IgnoredGenres: genres, Count: len(genres)})
	}
}

// replaceIgnoredGenres godoc
// @Summary Replace ignored genres
// @Description Replace the entire ignore_genres list in one bulk save. Use for the Save/Apply affordance on the Genres page.
// @Tags genres
// @Accept json
// @Produce json
// @Param request body genreListUpdateRequest true "Full list of ignored genres"
// @Success 200 {object} ignoredGenresResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/genres/ignored [put]
func replaceIgnoredGenres(deps GenreDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.ConfigStore == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "genre config store is not configured"})
			return
		}
		var req genreListUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		genres := normalizeGenreList(req.Genres)
		if err := deps.ConfigStore.SetIgnoreGenres(c.Request.Context(), genres); err != nil {
			c.JSON(storeErrorStatus(err), contracts.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, ignoredGenresResponse{IgnoredGenres: genres, Count: len(genres)})
	}
}

// addIgnoredGenre godoc
// @Summary Add ignored genre
// @Description Add a single genre to the ignore_genres list. Idempotent — adding an existing genre returns 200.
// @Tags genres
// @Accept json
// @Produce json
// @Param request body genreAddRequest true "Genre to ignore"
// @Success 201 {object} ignoredGenresResponse
// @Success 200 {object} ignoredGenresResponse "Already ignored"
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/genres/ignored [post]
func addIgnoredGenre(deps GenreDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.ConfigStore == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "genre config store is not configured"})
			return
		}
		var req genreAddRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		genre := strings.TrimSpace(req.Genre)
		if genre == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "genre is required"})
			return
		}
		result, changed, err := deps.ConfigStore.AddIgnoreGenre(c.Request.Context(), genre)
		if err != nil {
			c.JSON(storeErrorStatus(err), contracts.ErrorResponse{Error: err.Error()})
			return
		}
		status := http.StatusCreated
		if !changed {
			status = http.StatusOK
		}
		c.JSON(status, ignoredGenresResponse{IgnoredGenres: result, Count: len(result)})
	}
}

// deleteIgnoredGenre godoc
// @Summary Remove ignored genre
// @Description Remove a single genre from the ignore_genres list by name
// @Tags genres
// @Produce json
// @Param genre query string true "Genre name to remove from the ignore list"
// @Success 200 {object} ignoredGenresResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/genres/ignored [delete]
func deleteIgnoredGenre(deps GenreDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.ConfigStore == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "genre config store is not configured"})
			return
		}
		genre := strings.TrimSpace(c.Query("genre"))
		if genre == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "genre query parameter is required"})
			return
		}
		result, changed, err := deps.ConfigStore.RemoveIgnoreGenre(c.Request.Context(), genre)
		if err != nil {
			c.JSON(storeErrorStatus(err), contracts.ErrorResponse{Error: err.Error()})
			return
		}
		if !changed {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "genre is not in the ignore list"})
			return
		}
		c.JSON(http.StatusOK, ignoredGenresResponse{IgnoredGenres: result, Count: len(result)})
	}
}

// listFavoriteGenres godoc
// @Summary List favorite genres
// @Description Get the user's favorite genres (quick-apply list) configured on the Genres page
// @Tags genres
// @Produce json
// @Success 200 {object} favoriteGenresResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/genres/favorites [get]
func listFavoriteGenres(deps GenreDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.ConfigStore == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "genre config store is not configured"})
			return
		}
		genres, err := deps.ConfigStore.GetFavoriteGenres(c.Request.Context())
		if err != nil {
			c.JSON(storeErrorStatus(err), contracts.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, favoriteGenresResponse{Favorites: genres, Count: len(genres)})
	}
}

// replaceFavoriteGenres godoc
// @Summary Replace favorite genres
// @Description Replace the entire favorite genres list in one bulk save (the Save/Apply affordance on the Genres page)
// @Tags genres
// @Accept json
// @Produce json
// @Param request body genreListUpdateRequest true "Full list of favorite genres"
// @Success 200 {object} favoriteGenresResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/genres/favorites [put]
func replaceFavoriteGenres(deps GenreDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.ConfigStore == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "genre config store is not configured"})
			return
		}
		var req genreListUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		genres := normalizeGenreList(req.Genres)
		if err := deps.ConfigStore.SetFavoriteGenres(c.Request.Context(), genres); err != nil {
			c.JSON(storeErrorStatus(err), contracts.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, favoriteGenresResponse{Favorites: genres, Count: len(genres)})
	}
}

// addFavoriteGenre godoc
// @Summary Add favorite genre
// @Description Add a single genre to the favorites list. Idempotent — adding an existing favorite returns 200.
// @Tags genres
// @Accept json
// @Produce json
// @Param request body genreAddRequest true "Genre to favorite"
// @Success 201 {object} favoriteGenresResponse
// @Success 200 {object} favoriteGenresResponse "Already favorited"
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/genres/favorites [post]
func addFavoriteGenre(deps GenreDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.ConfigStore == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "genre config store is not configured"})
			return
		}
		var req genreAddRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		genre := strings.TrimSpace(req.Genre)
		if genre == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "genre is required"})
			return
		}
		result, changed, err := deps.ConfigStore.AddFavoriteGenre(c.Request.Context(), genre)
		if err != nil {
			c.JSON(storeErrorStatus(err), contracts.ErrorResponse{Error: err.Error()})
			return
		}
		status := http.StatusCreated
		if !changed {
			status = http.StatusOK
		}
		c.JSON(status, favoriteGenresResponse{Favorites: result, Count: len(result)})
	}
}

// deleteFavoriteGenre godoc
// @Summary Remove favorite genre
// @Description Remove a single genre from the favorites list by name
// @Tags genres
// @Produce json
// @Param genre query string true "Genre name to remove from favorites"
// @Success 200 {object} favoriteGenresResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/genres/favorites [delete]
func deleteFavoriteGenre(deps GenreDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.ConfigStore == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "genre config store is not configured"})
			return
		}
		genre := strings.TrimSpace(c.Query("genre"))
		if genre == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "genre query parameter is required"})
			return
		}
		result, changed, err := deps.ConfigStore.RemoveFavoriteGenre(c.Request.Context(), genre)
		if err != nil {
			c.JSON(storeErrorStatus(err), contracts.ErrorResponse{Error: err.Error()})
			return
		}
		if !changed {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "genre is not in the favorites list"})
			return
		}
		c.JSON(http.StatusOK, favoriteGenresResponse{Favorites: result, Count: len(result)})
	}
}

func normalizeGenreList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, g := range in {
		trimmed := strings.TrimSpace(g)
		if trimmed == "" {
			continue
		}
		if !containsString(out, trimmed) {
			out = append(out, trimmed)
		}
	}
	return out
}

func containsString(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}

func removeString(s []string, v string) []string {
	out := make([]string, 0, len(s))
	for _, item := range s {
		if item != v {
			out = append(out, item)
		}
	}
	return out
}

// storeErrorStatus maps a ConfigStore error to the appropriate HTTP status.
// A not-configured store (noop store writes, or a RuntimeGenreConfigStore
// whose runtime is not initialized) is a 503, consistent with the explicit
// nil-store guard and the swagger annotations; all other errors are 500.
func storeErrorStatus(err error) int {
	if errors.Is(err, ErrGenreConfigStoreNotConfigured) {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}
