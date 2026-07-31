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
