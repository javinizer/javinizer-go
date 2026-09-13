package actress

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/javinizer/javinizer-go/internal/database"
)

const errorKey = "error"

// ListCandidates handles GET /actresses/candidates — quarantined scrape-created identities.
//
//	@Summary		List candidate identities
//	@Description	Returns quarantined identities created by scrape resolution misses.
//	@Tags			actresses
//	@Produce		json
//	@Param			limit	query		int	false	"Max results"	default(50)
//	@Param			offset	query		int	false	"Skip results"	default(0)
//	@Success		200		{object}	object{candidates=[]models.Actress,total=int}
//	@Failure		500		{object}	contracts.ErrorResponse
//	@Router			/api/v1/actresses/candidates [get]
func ListCandidates(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
		candidates, err := deps.ActressRepo.ListCandidates(c.Request.Context(), limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: err.Error()})
			return
		}
		count, err := deps.ActressRepo.CountCandidates(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"candidates": candidates, "total": count})
	}
}

// PromoteCandidate handles POST /actresses/candidates/:id/promote — user confirms
// canonical fields; the candidate becomes a verified user-owned identity.
//
//	@Summary		Promote a candidate identity
//	@Description	Confirms canonical fields and marks the candidate verified and user-owned.
//	@Tags			actresses
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int					true	"Candidate ID"
//	@Param			request	body		object{first_name=string,last_name=string,japanese_name=string,thumb_url=string}	false	"Canonical fields (empty = keep scraped values)"
//	@Success		200		{object}	models.Actress
//	@Failure		400		{object}	contracts.ErrorResponse
//	@Failure		404		{object}	contracts.ErrorResponse
//	@Failure		409		{object}	contracts.ErrorResponse
//	@Failure		500		{object}	contracts.ErrorResponse
//	@Router			/api/v1/actresses/candidates/{id}/promote [post]
func PromoteCandidate(deps ActressDeps) gin.HandlerFunc {
	type promoteRequest struct {
		FirstName    string `json:"first_name"`
		LastName     string `json:"last_name"`
		JapaneseName string `json:"japanese_name"`
		ThumbURL     string `json:"thumb_url"`
	}
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "invalid candidate id"})
			return
		}
		var req promoteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
			return
		}
		existing, err := deps.ActressRepo.FindByID(c.Request.Context(), uint(id))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{errorKey: "candidate not found"})
			return
		}
		if existing.Verified {
			c.JSON(http.StatusConflict, gin.H{errorKey: "identity is already verified"})
			return
		}
		first, last, jp := req.FirstName, req.LastName, req.JapaneseName
		if first == "" && last == "" && jp == "" {
			first, last, jp = existing.FirstName, existing.LastName, existing.JapaneseName
		}
		thumb := req.ThumbURL
		if thumb == "" {
			thumb = existing.ThumbURL
		}
		if err := deps.ActressRepo.PromoteCandidate(c.Request.Context(), uint(id), first, last, jp, thumb); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: err.Error()})
			return
		}
		updated, err := deps.ActressRepo.FindByID(c.Request.Context(), uint(id))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: err.Error()})
			return
		}
		c.JSON(http.StatusOK, updated)
	}
}

// ResolveCollision handles POST /actresses/collisions/:id/resolve — user-only
// collision resolution with the four outcomes, applied atomically.
//
//	@Summary		Resolve a collision
//	@Description	Applies one of keep_identity, adopt_canonical, adopt_alias, or reassign atomically.
//	@Tags			actresses
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"Collision ID"
//	@Param			request	body		object{resolution=string,target_actress_id=int}	true	"Resolution outcome"
//	@Success		200		{object}	object{resolved=bool,remaining_open=int}
//	@Failure		400		{object}	contracts.ErrorResponse
//	@Failure		404		{object}	contracts.ErrorResponse
//	@Failure		409		{object}	contracts.ErrorResponse
//	@Failure		500		{object}	contracts.ErrorResponse
//	@Router			/api/v1/actresses/collisions/{id}/resolve [post]
func ResolveCollision(deps ActressDeps) gin.HandlerFunc {
	type resolveRequest struct {
		Resolution      string `json:"resolution"`
		TargetActressID uint   `json:"target_actress_id"`
	}
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "invalid collision id"})
			return
		}
		var req resolveRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
			return
		}
		if deps.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "database not configured"})
			return
		}
		service := database.NewCollisionService(deps.DB)
		remaining, err := service.Resolve(c.Request.Context(), uint(id), req.Resolution, req.TargetActressID)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, gin.H{errorKey: err.Error()})
				return
			}
			if errors.Is(err, database.ErrCollisionNotOpen) {
				c.JSON(http.StatusConflict, gin.H{errorKey: err.Error()})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"resolved": true, "remaining_open": remaining})
	}
}

// ListCollisions handles GET /actresses/collisions?movie_id= — open collisions.
//
//	@Summary		List open collisions
//	@Description	Returns open scrape-vs-identity collisions for a movie.
//	@Tags			actresses
//	@Produce		json
//	@Param			movie_id	query		string	true	"Movie content ID"
//	@Success		200			{object}	object{collisions=[]models.CreditCollision}
//	@Failure		400			{object}	contracts.ErrorResponse
//	@Failure		500			{object}	contracts.ErrorResponse
//	@Router			/api/v1/actresses/collisions [get]
func ListCollisions(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		movieID := c.Query("movie_id")
		if movieID == "" {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "movie_id is required"})
			return
		}
		collisions, err := deps.CreditCollisionRepo.ListOpenByMovie(c.Request.Context(), movieID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"collisions": collisions})
	}
}

// UpdateCreditOverride handles POST /actresses/credits/:id/override — per-movie display override.
//
//	@Summary		Set a credit display override
//	@Description	Overrides one movie's displayed actress name without mutating the shared identity.
//	@Tags			actresses
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int								true	"Credit ID"
//	@Param			request	body		object{override_name=string,user_override=bool}	true	"Override payload"
//	@Success		200		{object}	object{ok=bool}
//	@Failure		400		{object}	contracts.ErrorResponse
//	@Failure		404		{object}	contracts.ErrorResponse
//	@Failure		500		{object}	contracts.ErrorResponse
//	@Router			/api/v1/actresses/credits/{id}/override [post]
func UpdateCreditOverride(deps ActressDeps) gin.HandlerFunc {
	type overrideRequest struct {
		OverrideName string `json:"override_name"`
		UserOverride bool   `json:"user_override"`
	}
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "invalid credit id"})
			return
		}
		var req overrideRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
			return
		}
		if deps.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "database not configured"})
			return
		}
		service := database.NewCollisionService(deps.DB)
		if err := service.UpdateCreditOverride(c.Request.Context(), uint(id), req.OverrideName, req.UserOverride); err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, gin.H{errorKey: err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// SuppressCredit handles POST /actresses/credits/:id/suppress — user removal tombstone.
//
//	@Summary		Suppress a credit
//	@Description	Marks a credit user-removed so later scrapes cannot resurrect it.
//	@Tags			actresses
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int						true	"Credit ID"
//	@Param			request	body		object{suppressed=bool}	true	"Suppression state"
//	@Success		200		{object}	object{ok=bool}
//	@Failure		400		{object}	contracts.ErrorResponse
//	@Failure		404		{object}	contracts.ErrorResponse
//	@Failure		500		{object}	contracts.ErrorResponse
//	@Router			/api/v1/actresses/credits/{id}/suppress [post]
func SuppressCredit(deps ActressDeps) gin.HandlerFunc {
	type suppressRequest struct {
		Suppressed bool `json:"suppressed"`
	}
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "invalid credit id"})
			return
		}
		var req suppressRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: err.Error()})
			return
		}
		if deps.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: "database not configured"})
			return
		}
		service := database.NewCollisionService(deps.DB)
		if err := service.SetCreditSuppressed(c.Request.Context(), uint(id), req.Suppressed); err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, gin.H{errorKey: err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{errorKey: err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
