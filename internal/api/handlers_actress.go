package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
)

// searchActresses handles GET /api/v1/actresses/search?q=query
// @Summary Search actresses
// @Description Search for actresses by name (first, last, or Japanese)
// @Tags actress
// @Produce json
// @Param q query string true "Search query"
// @Success 200 {array} models.Actress
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/search [get]
func searchActresses(actressRepo *database.ActressRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Query("q")

		// Empty query is allowed - returns all actresses
		// Search using GORM with LIKE query for all name fields
		// This searches in first_name, last_name, and japanese_name
		actresses, err := actressRepo.Search(query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error: err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, actresses)
	}
}
