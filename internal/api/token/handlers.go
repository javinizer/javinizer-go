package token

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

type createTokenRequest struct {
	Name string `json:"name"`
}

type tokenResponse struct {
	Token       string     `json:"token"`
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

type tokenListResponse struct {
	Tokens []tokenListItem `json:"tokens"`
	Count  int             `json:"count"`
}

type tokenListItem struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

// createToken godoc
// @Summary Create API token
// @Description Create a new API token with an optional name
// @Tags tokens
// @Accept json
// @Produce json
// @Param request body createTokenRequest true "Token details"
// @Success 201 {object} tokenResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/tokens [post]
func createToken(svc *TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createTokenRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		token, fullToken, err := svc.Create(c.Request.Context(), req.Name)
		if err != nil {
			core.RespondInternalError(c, err)
			return
		}

		c.JSON(http.StatusCreated, tokenResponse{
			Token:       fullToken,
			ID:          token.ID,
			Name:        token.Name,
			TokenPrefix: token.TokenPrefix,
			CreatedAt:   token.CreatedAt,
			LastUsedAt:  token.LastUsedAt,
		})
	}
}

// listTokens godoc
// @Summary List API tokens
// @Description List all active (non-revoked) API tokens
// @Tags tokens
// @Produce json
// @Success 200 {object} tokenListResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/tokens [get]
func listTokens(svc *TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokens, err := svc.List(c.Request.Context())
		if err != nil {
			core.RespondInternalError(c, err)
			return
		}

		items := make([]tokenListItem, len(tokens))
		for i, t := range tokens {
			items[i] = tokenListItem{
				ID:          t.ID,
				Name:        t.Name,
				TokenPrefix: t.TokenPrefix,
				CreatedAt:   t.CreatedAt,
				LastUsedAt:  t.LastUsedAt,
			}
		}

		c.JSON(http.StatusOK, tokenListResponse{
			Tokens: items,
			Count:  len(items),
		})
	}
}

// revokeToken godoc
// @Summary Revoke API token
// @Description Revoke an API token by ID, making it immediately invalid
// @Tags tokens
// @Produce json
// @Param id path string true "Token ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/tokens/{id} [delete]
func revokeToken(svc *TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		if err := svc.Revoke(c.Request.Context(), id); err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "token not found"})
				return
			}
			core.RespondInternalError(c, err)
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "token revoked", "id": id})
	}
}

// regenerateToken godoc
// @Summary Regenerate API token
// @Description Regenerate an API token by ID, returning a new token value and invalidating the old one
// @Tags tokens
// @Produce json
// @Param id path string true "Token ID"
// @Success 200 {object} tokenResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/tokens/{id}/regenerate [post]
func regenerateToken(svc *TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		token, fullToken, err := svc.Regenerate(c.Request.Context(), id)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "token not found"})
				return
			}
			core.RespondInternalError(c, err)
			return
		}

		c.JSON(http.StatusOK, tokenResponse{
			Token:       fullToken,
			ID:          token.ID,
			Name:        token.Name,
			TokenPrefix: token.TokenPrefix,
			CreatedAt:   token.CreatedAt,
			LastUsedAt:  token.LastUsedAt,
		})
	}
}
