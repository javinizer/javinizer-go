package actress

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

type actressRequest struct {
	DMMID        int    `json:"dmm_id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	JapaneseName string `json:"japanese_name"`
	ThumbURL     string `json:"thumb_url"`
	Aliases      string `json:"aliases"`
}

type actressesResponse struct {
	Actresses []models.Actress `json:"actresses"`
	Count     int              `json:"count"`
	Total     int64            `json:"total"`
	Limit     int              `json:"limit"`
	Offset    int              `json:"offset"`
}

func normalizeActressRequest(req *actressRequest) {
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.JapaneseName = strings.TrimSpace(req.JapaneseName)
	req.ThumbURL = strings.TrimSpace(req.ThumbURL)
	req.Aliases = strings.TrimSpace(req.Aliases)
}

func validateActressRequest(req *actressRequest) error {
	if req.DMMID < 0 {
		return errors.New("dmm_id must be greater than or equal to 0")
	}
	if req.FirstName == "" && req.JapaneseName == "" {
		return errors.New("either first_name or japanese_name is required")
	}
	return nil
}

func parseSort(c *gin.Context) (string, string, error) {
	sortBy := strings.TrimSpace(strings.ToLower(c.Query("sort_by")))
	sortOrder := strings.TrimSpace(strings.ToLower(c.Query("sort_order")))

	if sortBy == "" {
		sortBy = "name"
	}
	if sortOrder == "" {
		sortOrder = "asc"
	}

	validSortColumns := map[string]bool{
		"id": true, "dmm_id": true, "japanese_name": true,
		"first_name": true, "last_name": true,
		"created_at": true, "updated_at": true, "name": true,
	}
	if !validSortColumns[sortBy] {
		return "", "", fmt.Errorf("invalid sort_by value: %q", sortBy)
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		return "", "", fmt.Errorf("invalid sort_order value: %q", sortOrder)
	}
	return sortBy, sortOrder, nil
}

func parseActressID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "invalid actress id"})
		return 0, false
	}
	return uint(id), true
}

// listActresses godoc
// @Summary List actresses
// @Description Get a paginated list of actresses with optional search query and sorting
// @Tags actress
// @Produce json
// @Param q query string false "Search query"
// @Param sort_by query string false "Sort column (id, dmm_id, japanese_name, first_name, last_name, created_at, updated_at, name)" default(name)
// @Param sort_order query string false "Sort direction (asc, desc)" default(asc)
// @Param limit query int false "Max results" default(50)
// @Param offset query int false "Skip results" default(0)
// @Param include_translations query string false "Language code to include translations for (e.g., 'en')"
// @Success 200 {object} actressesResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses [get]
func listActresses(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, offset := core.ParsePagination(c, 50, 500)
		query := strings.TrimSpace(c.Query("q"))
		sortBy, sortOrder, err := parseSort(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		var actresses []models.Actress
		var total int64

		repo := deps.ActressRepo
		if query == "" {
			total, err = repo.Count(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
				return
			}

			actresses, err = repo.ListSorted(c.Request.Context(), limit, offset, sortBy, sortOrder)
			if err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
				return
			}
		} else {
			total, err = repo.CountSearch(c.Request.Context(), query)
			if err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
				return
			}

			actresses, err = repo.SearchPagedSorted(c.Request.Context(), query, limit, offset, sortBy, sortOrder)
			if err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
				return
			}
		}

		includeLang := strings.TrimSpace(c.Query("include_translations"))
		if includeLang != "" {
			actressIDs := make([]uint, len(actresses))
			for i, a := range actresses {
				actressIDs[i] = a.ID
			}
			transMap, err := deps.safeFindTranslationsByIDs(c.Request.Context(), actressIDs, includeLang)
			if err != nil {
				// Translation lookup is best-effort; log and continue
			} else if transMap != nil {
				for i := range actresses {
					actresses[i].Translations = transMap[actresses[i].ID]
				}
			}
		}

		c.JSON(http.StatusOK, actressesResponse{
			Actresses: actresses,
			Count:     len(actresses),
			Total:     total,
			Limit:     limit,
			Offset:    offset,
		})
	}
}

// getActress godoc
// @Summary Get actress by ID
// @Description Retrieve a single actress by their database ID
// @Tags actress
// @Produce json
// @Param id path uint true "models.Actress ID"
// @Param include_translations query string false "Language code to include translations for (e.g., 'en')"
// @Success 200 {object} models.Actress
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/{id} [get]
func getActress(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseActressID(c)
		if !ok {
			return
		}

		actress, err := deps.ActressRepo.FindByID(c.Request.Context(), id)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "actress not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		includeLang := strings.TrimSpace(c.Query("include_translations"))
		if includeLang != "" {
			t, findErr := deps.safeFindTranslationByActress(c.Request.Context(), actress.ID, includeLang)
			if findErr != nil {
				// best-effort translation lookup
			} else if t != nil {
				actress.Translations = append(actress.Translations, *t)
			}
		}

		c.JSON(http.StatusOK, actress)
	}
}

// createActress godoc
// @Summary Create actress
// @Description Create a new actress record with the provided details
// @Tags actress
// @Accept json
// @Produce json
// @Param request body actressRequest true "models.Actress details"
// @Success 201 {object} models.Actress
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses [post]
func createActress(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req actressRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		normalizeActressRequest(&req)
		if err := validateActressRequest(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		actress := &models.Actress{
			DMMID:        req.DMMID,
			FirstName:    req.FirstName,
			LastName:     req.LastName,
			JapaneseName: req.JapaneseName,
			ThumbURL:     req.ThumbURL,
			Aliases:      req.Aliases,
		}

		if err := deps.ActressRepo.Create(c.Request.Context(), actress); err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusCreated, actress)
	}
}

// updateActress godoc
// @Summary Update actress
// @Description Update an existing actress record by ID
// @Tags actress
// @Accept json
// @Produce json
// @Param id path uint true "models.Actress ID"
// @Param request body actressRequest true "Updated actress details"
// @Success 200 {object} models.Actress
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/{id} [put]
func updateActress(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseActressID(c)
		if !ok {
			return
		}

		existing, err := deps.ActressRepo.FindByID(c.Request.Context(), id)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "actress not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		var req actressRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		normalizeActressRequest(&req)
		if err := validateActressRequest(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		existing.DMMID = req.DMMID
		existing.FirstName = req.FirstName
		existing.LastName = req.LastName
		existing.JapaneseName = req.JapaneseName
		existing.ThumbURL = req.ThumbURL
		existing.Aliases = req.Aliases

		if err := deps.ActressRepo.Update(c.Request.Context(), existing); err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, existing)
	}
}

// deleteActress godoc
// @Summary Delete actress
// @Description Delete an actress record by ID
// @Tags actress
// @Produce json
// @Param id path uint true "models.Actress ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/{id} [delete]
func deleteActress(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseActressID(c)
		if !ok {
			return
		}

		existing, err := deps.ActressRepo.FindByID(c.Request.Context(), id)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "actress not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		if err := deps.ActressRepo.Delete(c.Request.Context(), id); err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "actress deleted", "id": existing.ID})
	}
}

type actressesImportRequest struct {
	Actresses []actressImportItem `json:"actresses"`
}

type actressImportItem struct {
	DMMID        int    `json:"dmm_id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	JapaneseName string `json:"japanese_name"`
	ThumbURL     string `json:"thumb_url"`
	Aliases      string `json:"aliases"`
}

type importSummaryResponse struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Errors   int `json:"errors"`
}

func exportActresses(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Stream the actress table to the response in chunks instead of
		// materializing the entire table in memory. For a library with 100k+
		// actresses, buffering every record would allocate hundreds of
		// megabytes and risk OOM.
		const chunkSize = 1000
		c.Header("Content-Type", "application/json")
		enc := json.NewEncoder(c.Writer)
		first := true
		if _, err := c.Writer.Write([]byte("[")); err != nil {
			return
		}
		offset := 0
		for {
			chunk, err := deps.ActressRepo.List(c.Request.Context(), chunkSize, offset)
			if err != nil {
				// Once the header/body has started we can no longer swap to a
				// 500; log and stop writing so the client sees a truncated
				// stream rather than a corrupt buffer.
				if first {
					core.RespondInternalError(c, err)
					return
				}
				logging.Errorf("exportActresses: streaming aborted: %v", err)
				return
			}
			if len(chunk) == 0 {
				break
			}
			for _, actress := range chunk {
				if !first {
					if _, err := c.Writer.Write([]byte(",")); err != nil {
						return
					}
				}
				if err := enc.Encode(actress); err != nil {
					return
				}
				first = false
			}
			offset += len(chunk)
		}
		if _, err := c.Writer.Write([]byte("]")); err != nil {
			return
		}
	}
}

func importActresses(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)

		var req actressesImportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		repo := deps.ActressRepo
		var imported, skipped, errorsCount int

		for _, item := range req.Actresses {
			firstName := strings.TrimSpace(item.FirstName)
			lastName := strings.TrimSpace(item.LastName)
			japaneseName := strings.TrimSpace(item.JapaneseName)
			thumbURL := strings.TrimSpace(item.ThumbURL)
			aliases := strings.TrimSpace(item.Aliases)

			if firstName == "" && japaneseName == "" {
				errorsCount++
				continue
			}

			if item.DMMID < 0 {
				errorsCount++
				continue
			}

			existing, err := repo.FindByJapaneseNameAndDMMID(c.Request.Context(), japaneseName, item.DMMID)
			if err != nil && !database.IsNotFound(err) && !errors.Is(err, database.ErrInvalidLookup) {
				errorsCount++
				continue
			}

			if existing == nil {
				actress := &models.Actress{
					DMMID:        item.DMMID,
					FirstName:    firstName,
					LastName:     lastName,
					JapaneseName: japaneseName,
					ThumbURL:     thumbURL,
					Aliases:      aliases,
				}
				if err := repo.Create(c.Request.Context(), actress); err != nil {
					errorsCount++
					continue
				}
				imported++
			} else {
				changed := existing.FirstName != firstName ||
					existing.LastName != lastName ||
					existing.ThumbURL != thumbURL ||
					existing.Aliases != aliases

				if changed {
					existing.FirstName = firstName
					existing.LastName = lastName
					existing.ThumbURL = thumbURL
					existing.Aliases = aliases

					if err := repo.Update(c.Request.Context(), existing); err != nil {
						errorsCount++
						continue
					}
					imported++
				} else {
					skipped++
				}
			}
		}

		c.JSON(http.StatusOK, importSummaryResponse{
			Imported: imported,
			Skipped:  skipped,
			Errors:   errorsCount,
		})
	}
}
