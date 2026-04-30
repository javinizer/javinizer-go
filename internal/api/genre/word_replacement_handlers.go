package genre

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

const maxImportBodySize = 10 << 20

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
		if err != nil && !database.IsNotFound(err) {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		if existing != nil {
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
		if err != nil {
			if !database.IsNotFound(err) {
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}
		}
		if existing == nil {
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
		idStr := c.Query("id")
		original := strings.TrimSpace(c.Query("original"))

		if idStr != "" {
			var id uint64
			if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "id must be a number"})
				return
			}

			replacement, err := deps.WordReplacementRepo.FindByID(uint(id))
			if err != nil {
				if database.IsNotFound(err) {
					c.JSON(http.StatusNotFound, ErrorResponse{Error: "word replacement not found"})
					return
				}
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}

			if err := deps.WordReplacementRepo.DeleteByID(uint(id)); err != nil {
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"message": "word replacement deleted", "original": replacement.Original})
			return
		}

		if original != "" {
			existing, err := deps.WordReplacementRepo.FindByOriginal(original)
			if err != nil {
				if database.IsNotFound(err) {
					c.JSON(http.StatusNotFound, ErrorResponse{Error: "word replacement not found"})
					return
				}
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}

			if err := deps.WordReplacementRepo.Delete(original); err != nil {
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"message": "word replacement deleted", "original": existing.Original})
			return
		}

		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "id or original query parameter is required"})
	}
}

type wordReplacementImportRequest struct {
	Replacements    []wordReplacementCreateRequest `json:"replacements"`
	IncludeDefaults bool                           `json:"includeDefaults"`
}

func exportWordReplacements(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		replacements, err := deps.WordReplacementRepo.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, replacements)
	}
}

func importWordReplacements(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImportBodySize)

		var req wordReplacementImportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		var imported, skipped, errorsCount int

		for _, item := range req.Replacements {
			orig := strings.TrimSpace(item.Original)
			repl := strings.TrimSpace(item.Replacement)

			if orig == "" {
				errorsCount++
				continue
			}

			if !req.IncludeDefaults && database.IsDefaultWordReplacement(orig) {
				skipped++
				continue
			}

			existing, err := deps.WordReplacementRepo.FindByOriginal(orig)
			if err != nil && !database.IsNotFound(err) {
				errorsCount++
				continue
			}

			var changed bool

			if existing == nil {
				replacement := &models.WordReplacement{
					Original:    orig,
					Replacement: repl,
				}
				if err := deps.WordReplacementRepo.Create(replacement); err != nil {
					errorsCount++
					continue
				}
				changed = true
			} else if existing.Replacement != repl {
				existing.Replacement = repl
				if err := deps.WordReplacementRepo.Upsert(existing); err != nil {
					errorsCount++
					continue
				}
				changed = true
			}

			if changed {
				imported++
			} else {
				skipped++
			}
		}

		c.JSON(http.StatusOK, importSummaryResponse{
			Imported: imported,
			Skipped:  skipped,
			Errors:   errorsCount,
		})
	}
}
