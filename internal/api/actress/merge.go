package actress

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"gorm.io/gorm"
)

func writeActressMergeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, database.ErrActressMergeInvalidID),
		errors.Is(err, database.ErrActressMergeSameID),
		errors.Is(err, database.ErrActressMergeInvalidField),
		errors.Is(err, database.ErrActressMergeInvalidDecision):
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "actress not found"})
	case errors.Is(err, database.ErrActressMergeUniqueConstraint):
		c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
}

// previewActressMerge handles POST /api/v1/actresses/merge/preview.
// @Summary Preview actress merge
// @Description Build a merge preview and field conflict set for target/source actress IDs.
// @Tags actress
// @Accept json
// @Produce json
// @Param request body ActressMergePreviewRequest true "Merge preview request"
// @Success 200 {object} ActressMergePreviewResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/merge/preview [post]
func previewActressMerge(actressRepo *database.ActressRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ActressMergePreviewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		preview, err := actressRepo.PreviewMerge(req.TargetID, req.SourceID)
		if err != nil {
			writeActressMergeError(c, err)
			return
		}

		conflicts := make([]ActressMergeConflict, 0, len(preview.Conflicts))
		for _, conflict := range preview.Conflicts {
			conflicts = append(conflicts, ActressMergeConflict{
				Field:             conflict.Field,
				TargetValue:       conflict.TargetValue,
				SourceValue:       conflict.SourceValue,
				DefaultResolution: conflict.DefaultResolution,
			})
		}

		c.JSON(http.StatusOK, ActressMergePreviewResponse{
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
// @Param request body ActressMergeRequest true "Merge request"
// @Success 200 {object} ActressMergeResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/merge [post]
func mergeActresses(actressRepo *database.ActressRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ActressMergeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		result, err := actressRepo.Merge(req.TargetID, req.SourceID, req.Resolutions)
		if err != nil {
			writeActressMergeError(c, err)
			return
		}

		c.JSON(http.StatusOK, ActressMergeResponse{
			MergedActress:     result.MergedActress,
			MergedFromID:      result.MergedFromID,
			UpdatedMovies:     result.UpdatedMovies,
			ConflictsResolved: result.ConflictsResolved,
			AliasesAdded:      result.AliasesAdded,
		})
	}
}
