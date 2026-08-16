package actress

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
)

type actressSyncCandidateItem struct {
	ID           uint   `json:"id"`
	JapaneseName string `json:"japanese_name"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	DMMID        int    `json:"dmm_id"`
	ThumbURL     string `json:"thumb_url"`
}

type actressSyncCandidatesResponse struct {
	Items  []actressSyncCandidateItem `json:"items"`
	Total  int                        `json:"total"`
	Limit  int                        `json:"limit"`
	Offset int                        `json:"offset"`
}

// listActressSyncCandidates handles GET /api/v1/actresses/sync-candidates.
// @Summary List actresses eligible for metadata sync
// @Description Return actresses with a DMM ID that lack thumbnail, Japanese name, or romanized names, plus named actresses without a DMM ID pending identity resolution.
// @Tags actress
// @Produce json
// @Param filter query string false "Registered actress filter"
// @Param limit query int false "Max results (1-1000; default 500)" default(500)
// @Param offset query int false "Pagination offset" default(0)
// @Success 200 {object} actressSyncCandidatesResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-candidates [get]
func listActressSyncCandidates(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rt == nil || rt.Deps() == nil || rt.Deps().CoreDeps == nil || rt.Deps().CoreDeps.DB == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "actress sync repository is unavailable"})
			return
		}
		limit, err := parseActressSyncLimit(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		offset := 0
		if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
			offset, err = strconv.Atoi(raw)
			if err != nil || offset < 0 {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "offset must be a non-negative integer"})
				return
			}
		}
		filter := strings.TrimSpace(c.Query("filter"))
		if filter != "" {
			if _, ok := database.ValidActressFilter(filter); !ok {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "unknown filter"})
				return
			}
		}
		actresses, total, err := database.NewActressRepository(rt.Deps().CoreDeps.DB).ListSyncCandidatesPaged(c.Request.Context(), filter, limit, offset)
		if err != nil {
			core.RespondInternalError(c, err)
			return
		}
		items := make([]actressSyncCandidateItem, len(actresses))
		for i, a := range actresses {
			items[i] = actressSyncCandidateItem{
				ID:           a.ID,
				JapaneseName: a.JapaneseName,
				FirstName:    a.FirstName,
				LastName:     a.LastName,
				DMMID:        a.DMMID,
				ThumbURL:     a.ThumbURL,
			}
		}
		c.JSON(http.StatusOK, actressSyncCandidatesResponse{Items: items, Total: total, Limit: limit, Offset: offset})
	}
}
