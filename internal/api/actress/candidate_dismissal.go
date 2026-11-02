package actress

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
)

// DismissCandidate hides an unverified identity from the pending review queue.
// It does not remove credits, resolve collisions, or change the identity.
//
// @Summary Dismiss a candidate from review
// @Description Reversibly hides an unverified candidate from the pending queue without changing actor or movie data.
// @Tags actresses
// @Produce json
// @Param id path int true "Candidate ID"
// @Success 200 {object} object{ok=bool}
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/candidates/{id}/dismiss [post]
func DismissCandidate(deps ActressDeps) gin.HandlerFunc {
	return setCandidateDismissed(deps, true)
}

// RestoreCandidate returns a dismissed identity to the pending review queue.
//
// @Summary Restore a dismissed candidate
// @Description Restores an unverified candidate to review without changing actor or movie data.
// @Tags actresses
// @Produce json
// @Param id path int true "Candidate ID"
// @Success 200 {object} object{ok=bool}
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/candidates/{id}/restore [post]
func RestoreCandidate(deps ActressDeps) gin.HandlerFunc {
	return setCandidateDismissed(deps, false)
}

func setCandidateDismissed(deps ActressDeps, dismissed bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, strconv.IntSize)
		if err != nil || id == 0 {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "invalid candidate id"})
			return
		}
		if deps.DB == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: databaseNotConfiguredError})
			return
		}
		if err := database.NewActressRepository(deps.DB).SetCandidateDismissed(c.Request.Context(), uint(id), dismissed); err != nil {
			switch {
			case database.IsNotFound(err):
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "candidate not found"})
			case errors.Is(err, database.ErrCandidateAlreadyVerified):
				c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: err.Error()})
			case errors.Is(err, database.ErrInvalidLookup):
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			}
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
