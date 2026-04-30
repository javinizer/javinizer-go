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

type wordReplacementImportRequest struct {
	Replacements   []wordReplacementCreateRequest `json:"replacements"`
	IncludeDefaults bool                           `json:"includeDefaults"`
}

var defaultWordReplacements = map[string]bool{
	"[Recommended For Smartphones] ": true,
	"A*****t": true, "A*****ted": true, "A****p": true, "A***e": true,
	"B***d": true, "B**d": true, "C***d": true,
	"D******ed": true, "D******eful": true, "D***k": true,
	"D***king": true, "D**g": true, "D**gged": true,
	"F***": true, "F*****g": true, "F***e": true,
	"G*********d": true, "G*******g": true, "G******g": true,
	"H*********n": true, "H*******ed": true, "H*******m": true,
	"I****t": true, "I****tuous": true,
	"K****p": true, "K**l": true, "K**ler": true, "K*d": true,
	"Ko**ji": true, "Lo**ta": true,
	"M******r": true, "M****t": true, "M****ted": true, "M****ter": true, "M****ting": true,
	"P****h": true, "P****hment": true,
	"P*A": true,
	"R****g": true, "R**e": true, "R**ed": true, "R*pe": true,
	"S*********l": true, "S*********ls": true, "S********l": true,
	"S********n": true, "S******g": true, "S*****t": true,
	"S***e": true, "S***p": true, "S**t": true,
	"Sch**l": true, "Sch**lgirl": true, "Sch**lgirls": true,
	"SK**lful": true, "SK**ls": true,
	"StepB****************r": true, "StepM************n": true,
	"StumB**d": true,
	"T*****e": true,
	"U*********sly": true, "U**verse": true,
	"V*****e": true, "V*****ed": true, "V*****es": true, "V*****t": true,
	"Y********l": true,
	"D******e": true,
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
		var req wordReplacementImportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		var imported, skipped, errors int

		for _, item := range req.Replacements {
			orig := strings.TrimSpace(item.Original)
			repl := strings.TrimSpace(item.Replacement)

			if orig == "" {
				errors++
				continue
			}

			if !req.IncludeDefaults && defaultWordReplacements[orig] {
				skipped++
				continue
			}

			existing, err := deps.WordReplacementRepo.FindByOriginal(orig)
			if err != nil {
				errors++
				continue
			}

			var changed bool

			if existing == nil {
				replacement := &models.WordReplacement{
					Original:    orig,
					Replacement: repl,
				}
				if err := deps.WordReplacementRepo.Create(replacement); err != nil {
					errors++
					continue
				}
				changed = true
			} else if existing.Replacement != repl {
				existing.Replacement = repl
				if err := deps.WordReplacementRepo.Upsert(existing); err != nil {
					errors++
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
			Errors:   errors,
		})
	}
}
