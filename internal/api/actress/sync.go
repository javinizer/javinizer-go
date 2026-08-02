package actress

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

type actressSyncCandidatesResponse struct {
	IDs       []uint           `json:"ids"`
	Actresses []models.Actress `json:"actresses"`
	Total     int              `json:"total"`
}

// listActressSyncCandidates handles GET /api/v1/actresses/sync-candidates.
// @Summary List actresses eligible for metadata sync
// @Description Return actresses that have a DMM ID but lack thumbnail, Japanese name, or romanized name metadata.
// @Tags actress
// @Produce json
// @Success 200 {object} actressSyncCandidatesResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-candidates [get]
func listActressSyncCandidates(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rt == nil || rt.Deps() == nil || rt.Deps().CoreDeps == nil || rt.Deps().CoreDeps.DB == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "actress sync repository is unavailable"})
			return
		}
		actresses, err := database.NewActressRepository(rt.Deps().CoreDeps.DB).ListSyncCandidates(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		ids := make([]uint, len(actresses))
		for i := range actresses {
			ids[i] = actresses[i].ID
		}
		c.JSON(http.StatusOK, actressSyncCandidatesResponse{IDs: ids, Actresses: actresses, Total: len(actresses)})
	}
}
