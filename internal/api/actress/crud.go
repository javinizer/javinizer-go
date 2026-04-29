package actress

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
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
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid actress id"})
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
// @Success 200 {object} actressesResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses [get]
func listActresses(actressRepo *database.ActressRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, offset := core.ParsePagination(c, 50, 500)
		query := strings.TrimSpace(c.Query("q"))
		sortBy, sortOrder, err := parseSort(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		var actresses []models.Actress
		var total int64

		if query == "" {
			total, err = actressRepo.Count()
			if err != nil {
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}

			actresses, err = actressRepo.ListSorted(limit, offset, sortBy, sortOrder)
			if err != nil {
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}
		} else {
			total, err = actressRepo.CountSearch(query)
			if err != nil {
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}

			actresses, err = actressRepo.SearchPagedSorted(query, limit, offset, sortBy, sortOrder)
			if err != nil {
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
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
// @Param id path uint true "Actress ID"
// @Success 200 {object} Actress
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/{id} [get]
func getActress(actressRepo *database.ActressRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseActressID(c)
		if !ok {
			return
		}

		actress, err := actressRepo.FindByID(id)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, ErrorResponse{Error: "actress not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
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
// @Param request body actressRequest true "Actress details"
// @Success 201 {object} Actress
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses [post]
func createActress(actressRepo *database.ActressRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req actressRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		normalizeActressRequest(&req)
		if err := validateActressRequest(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
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

		if err := actressRepo.Create(actress); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
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
// @Param id path uint true "Actress ID"
// @Param request body actressRequest true "Updated actress details"
// @Success 200 {object} Actress
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/{id} [put]
func updateActress(actressRepo *database.ActressRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseActressID(c)
		if !ok {
			return
		}

		existing, err := actressRepo.FindByID(id)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, ErrorResponse{Error: "actress not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		var req actressRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		normalizeActressRequest(&req)
		if err := validateActressRequest(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		existing.DMMID = req.DMMID
		existing.FirstName = req.FirstName
		existing.LastName = req.LastName
		existing.JapaneseName = req.JapaneseName
		existing.ThumbURL = req.ThumbURL
		existing.Aliases = req.Aliases

		if err := actressRepo.Update(existing); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
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
// @Param id path uint true "Actress ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/{id} [delete]
func deleteActress(actressRepo *database.ActressRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseActressID(c)
		if !ok {
			return
		}

		existing, err := actressRepo.FindByID(id)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, ErrorResponse{Error: "actress not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		if err := actressRepo.Delete(id); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "actress deleted", "id": existing.ID})
	}
}
