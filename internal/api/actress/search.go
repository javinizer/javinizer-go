package actress

import (
	"net/http"

	"github.com/gin-gonic/gin"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// actressSearchResponse is a type alias used solely for swag documentation.
// swag cannot resolve models.Actress in {array} context, so we use a local type.
//
//nolint:unused
type actressSearchResponse = models.Actress

// searchActresses handles GET /api/v1/actresses/search?q=query
// @Summary Search actresses
// @Description Search for actresses by name (first, last, or Japanese)
// @Tags actress
// @Produce json
// @Param q query string false "Search query"
// @Success 200 {array} actressSearchResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/search [get]
func searchActresses(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Query("q")

		// Empty query is allowed - returns all actresses
		// Search using GORM with LIKE query for all name fields
		// This searches in first_name, last_name, and japanese_name
		actresses, err := deps.ActressRepo.Search(c.Request.Context(), query)
		if err != nil {
			logging.Errorf("actress search failed: %v", err)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "internal server error"})
			return
		}

		c.JSON(http.StatusOK, actresses)
	}
}
