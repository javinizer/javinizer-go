package genre

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/models"
)

type genreReplacementCreateRequest struct {
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

type genreReplacementListResponse struct {
	Replacements []models.GenreReplacement `json:"replacements"`
	Count        int                       `json:"count"`
	Total        int64                     `json:"total"`
	Limit        int                       `json:"limit"`
	Offset       int                       `json:"offset"`
}

func parsePagination(c *gin.Context) (int, int) {
	limit := 50
	offset := 0

	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > 500 {
				limit = 500
			}
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return limit, offset
}

func listGenreReplacements(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, offset := parsePagination(c)

		replacements, err := deps.GenreReplacementRepo.List()
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

		c.JSON(http.StatusOK, genreReplacementListResponse{
			Replacements: paged,
			Count:        len(paged),
			Total:        total,
			Limit:        limit,
			Offset:       offset,
		})
	}
}

func createGenreReplacement(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req genreReplacementCreateRequest
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
		if req.Replacement == "" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "replacement is required"})
			return
		}

		existing, err := deps.GenreReplacementRepo.FindByOriginal(req.Original)
		if err == nil && existing != nil {
			c.JSON(http.StatusOK, existing)
			return
		}

		replacement := &models.GenreReplacement{
			Original:    req.Original,
			Replacement: req.Replacement,
		}

		if err := deps.GenreReplacementRepo.Create(replacement); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusCreated, replacement)
	}
}

func deleteGenreReplacement(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		original := c.Param("original")

		existing, err := deps.GenreReplacementRepo.FindByOriginal(original)
		if err != nil {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "genre replacement not found"})
			return
		}

		if err := deps.GenreReplacementRepo.Delete(original); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "genre replacement deleted", "original": existing.Original})
	}
}
