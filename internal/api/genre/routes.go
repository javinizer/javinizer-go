package genre

import (
	"github.com/gin-gonic/gin"
)

// invalidateCaches is called by handlers after successful mutations to refresh
// the aggregator's replacement caches. This keeps GenreDeps as a pure
// data-access module — cache orchestration belongs to the handler layer.
type invalidateCaches func()

// RegisterRoutes registers all genre and word replacement routes.
// The invalidateCaches callback is called after each successful mutation
// so the aggregator picks up replacement changes.
func RegisterRoutes(protected *gin.RouterGroup, deps GenreDeps, invalidate invalidateCaches) {
	protected.GET("/genres", listGenres(deps))

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

	replacements := protected.Group("/genres/replacements")
	replacements.GET("", listGenreReplacements(deps))
	replacements.POST("", createGenreReplacement(deps, invalidate))
	replacements.PUT("", updateGenreReplacement(deps, invalidate))
	replacements.DELETE("", deleteGenreReplacement(deps, invalidate))
	replacements.GET("/export", exportGenreReplacements(deps))
	replacements.POST("/import", importGenreReplacements(deps, invalidate))

	wordReplacements := protected.Group("/words/replacements")
	wordReplacements.GET("", listWordReplacements(deps))
	wordReplacements.POST("", createWordReplacement(deps, invalidate))
	wordReplacements.PUT("", updateWordReplacement(deps, invalidate))
	wordReplacements.DELETE("", deleteWordReplacement(deps, invalidate))
	wordReplacements.GET("/export", exportWordReplacements(deps))
	wordReplacements.POST("/import", importWordReplacements(deps, invalidate))
}
