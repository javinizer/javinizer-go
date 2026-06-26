package actress

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func writeActressMergeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, database.ErrActressMergeInvalidID),
		errors.Is(err, database.ErrActressMergeSameID),
		errors.Is(err, database.ErrActressMergeInvalidField),
		errors.Is(err, database.ErrActressMergeInvalidDecision):
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
	case database.IsNotFound(err):
		c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "actress not found"})
	case errors.Is(err, database.ErrActressMergeUniqueConstraint):
		c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: err.Error()})
	default:
		core.RespondInternalError(c, err)
	}
}

// previewActressMerge handles POST /api/v1/actresses/merge/preview.
// @Summary Preview actress merge
// @Description Build a merge preview and field conflict set for target/source actress IDs.
// @Tags actress
// @Accept json
// @Produce json
// @Param request body contracts.ActressMergePreviewRequest true "Merge preview request"
// @Success 200 {object} contracts.ActressMergePreviewResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/merge/preview [post]
func previewActressMerge(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.ActressMergePreviewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		preview, err := deps.ActressRepo.PreviewMerge(c.Request.Context(), req.TargetID, req.SourceID)
		if err != nil {
			writeActressMergeError(c, err)
			return
		}

		conflicts := make([]contracts.ActressMergeConflict, 0, len(preview.Conflicts))
		for _, conflict := range preview.Conflicts {
			conflicts = append(conflicts, contracts.ActressMergeConflict{
				Field:             conflict.Field,
				TargetValue:       conflict.TargetValue,
				SourceValue:       conflict.SourceValue,
				DefaultResolution: conflict.DefaultResolution,
			})
		}

		c.JSON(http.StatusOK, contracts.ActressMergePreviewResponse{
			Target:             preview.Target,
			Source:             preview.Source,
			ProposedMerged:     preview.ProposedMerged,
			Conflicts:          conflicts,
			DefaultResolutions: preview.DefaultResolutions,
		})
	}
}

// mergeActresses handles POST /api/v1/actresses/merge.
// @Summary Merge duplicated actresses
// @Description Merge a source actress into a target actress with field-level target/source resolutions.
// @Tags actress
// @Accept json
// @Produce json
// @Param request body contracts.ActressMergeRequest true "Merge request"
// @Success 200 {object} contracts.ActressMergeResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/merge [post]
func mergeActresses(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.ActressMergeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		result, err := deps.ActressRepo.Merge(c.Request.Context(), req.TargetID, req.SourceID, req.Resolutions)
		if err != nil {
			writeActressMergeError(c, err)
			return
		}

		c.JSON(http.StatusOK, contracts.ActressMergeResponse{
			MergedActress:     result.MergedActress,
			MergedFromID:      result.MergedFromID,
			UpdatedMovies:     result.UpdatedMovies,
			ConflictsResolved: result.ConflictsResolved,
			AliasesAdded:      result.AliasesAdded,
		})
	}
}
