package genre

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type wordReplacementCreateRequest struct {
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

type wordReplacementUpdateRequest struct {
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

type wordReplacementListResponse struct {
	Replacements []models.WordReplacement `json:"replacements"`
	Count        int                      `json:"count"`
	Total        int64                    `json:"total"`
	Limit        int                      `json:"limit"`
	Offset       int                      `json:"offset"`
}

func listWordReplacements(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, offset := core.ParsePagination(c, 200, 500)

		replacements, err := deps.WordReplacementRepo.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		total := int64(len(replacements))

		end := offset + limit
		if end > len(replacements) {
			end = len(replacements)
		}
		if offset > len(replacements) {
			offset = len(replacements)
		}

		paged := replacements[offset:end]

		c.JSON(http.StatusOK, wordReplacementListResponse{
			Replacements: paged,
			Count:        len(paged),
			Total:        total,
			Limit:        limit,
			Offset:       offset,
		})
	}
}

func createWordReplacement(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req wordReplacementCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		req.Original = strings.TrimSpace(req.Original)
		req.Replacement = strings.TrimSpace(req.Replacement)

		if req.Original == "" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "original is required"})
			return
		}

		existing, err := deps.WordReplacementRepo.FindByOriginal(req.Original)
		if err == nil && existing != nil {
			c.JSON(http.StatusOK, existing)
			return
		}

		replacement := &models.WordReplacement{
			Original:    req.Original,
			Replacement: req.Replacement,
		}

		if err := deps.WordReplacementRepo.Create(replacement); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusCreated, replacement)
	}
}

func updateWordReplacement(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req wordReplacementUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		req.Original = strings.TrimSpace(req.Original)
		req.Replacement = strings.TrimSpace(req.Replacement)

		if req.Original == "" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "original is required"})
			return
		}

		existing, err := deps.WordReplacementRepo.FindByOriginal(req.Original)
		if err != nil || existing == nil {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "word replacement not found"})
			return
		}

		existing.Replacement = req.Replacement

		if err := deps.WordReplacementRepo.Upsert(existing); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, existing)
	}
}

func deleteWordReplacement(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		original := c.Query("original")
		if original == "" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "original query parameter is required"})
			return
		}

		existing, err := deps.WordReplacementRepo.FindByOriginal(original)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		if err != nil {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "word replacement not found"})
			return
		}

		if err := deps.WordReplacementRepo.Delete(original); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "word replacement deleted", "original": existing.Original})
	}
}
