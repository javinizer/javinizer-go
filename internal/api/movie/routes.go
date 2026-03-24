package movie

import "github.com/gin-gonic/gin"

func RegisterRoutes(protected *gin.RouterGroup, deps *ServerDependencies) {
	protected.POST("/scrape", scrapeMovie(deps))
	protected.GET("/movies/:id", getMovie(deps))
	protected.GET("/movies", listMovies(deps))
	protected.POST("/movies/:id/rescrape", rescrapeMovie(deps))
	protected.POST("/movies/:id/compare-nfo", compareNFO(deps))
}
