package genre

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

type genreReplacementCreateRequest struct {
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

type genreReplacementUpdateRequest struct {
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

// listGenreReplacements godoc
// @Summary List genre replacements
// @Description Get a paginated list of genre replacement rules
// @Tags genres
// @Produce json
// @Param limit query int false "Max results" default(50)
// @Param offset query int false "Skip results" default(0)
// @Success 200 {object} genreReplacementListResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/genres/replacements [get]
func listGenreReplacements(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, offset := core.ParsePagination(c, 500, 1000)

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

func updateGenreReplacement(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req genreReplacementUpdateRequest
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

		existing, err := deps.GenreReplacementRepo.FindByOriginal(req.Original)
		if err != nil {
			if !database.IsNotFound(err) {
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}
		}
		if existing == nil {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "genre replacement not found"})
			return
		}

		existing.Replacement = req.Replacement

		if err := deps.GenreReplacementRepo.Upsert(existing); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, existing)
	}
}

// createGenreReplacement godoc
// @Summary Create genre replacement
// @Description Create a new genre replacement rule mapping an original genre to a replacement
// @Tags genres
// @Accept json
// @Produce json
// @Param request body genreReplacementCreateRequest true "Genre replacement details"
// @Success 201 {object} GenreReplacement
// @Success 200 {object} GenreReplacement "Already exists"
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/genres/replacements [post]
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

		existing, err := deps.GenreReplacementRepo.FindByOriginal(req.Original)
		if err != nil && !database.IsNotFound(err) {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		if existing != nil {
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

// deleteGenreReplacement godoc
// @Summary Delete genre replacement
// @Description Delete a genre replacement rule by original genre name
// @Tags genres
// @Produce json
// @Param original query string true "Original genre name to delete"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/genres/replacements [delete]
func deleteGenreReplacement(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		original := strings.TrimSpace(c.Query("original"))
		if original == "" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "original query parameter is required"})
			return
		}

		existing, err := deps.GenreReplacementRepo.FindByOriginal(original)
		if err != nil && !database.IsNotFound(err) {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
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

type genreReplacementImportRequest struct {
	Replacements []genreReplacementCreateRequest `json:"replacements"`
}

type importSummaryResponse struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Errors   int `json:"errors"`
}

func exportGenreReplacements(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		replacements, err := deps.GenreReplacementRepo.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, replacements)
	}
}

func importGenreReplacements(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)

		var req genreReplacementImportRequest
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

			existing, err := deps.GenreReplacementRepo.FindByOriginal(orig)
			if err != nil && !database.IsNotFound(err) {
				errorsCount++
				continue
			}

			var changed bool

			if existing == nil {
				replacement := &models.GenreReplacement{
					Original:    orig,
					Replacement: repl,
				}
				if err := deps.GenreReplacementRepo.Create(replacement); err != nil {
					errorsCount++
					continue
				}
				changed = true
			} else if existing.Replacement != repl {
				existing.Replacement = repl
				if err := deps.GenreReplacementRepo.Upsert(existing); err != nil {
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
