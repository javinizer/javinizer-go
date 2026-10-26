package actresssync

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"gorm.io/gorm"
)

func fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, contracts.ErrorResponse{Code: code, Error: message})
}

func invalid(c *gin.Context) {
	fail(c, http.StatusBadRequest, "SYNC_INVALID_REQUEST", "invalid offline name sync request")
}

func unavailable(c *gin.Context) {
	fail(c, http.StatusServiceUnavailable, "SYNC_UNAVAILABLE", "offline name sync is unavailable")
}

// Storage and artifact errors can contain SQL, filesystem paths, or internal
// fencing values. Only enumerated stable categories cross the HTTP boundary.
func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, actressnamesync.ErrInvalid):
		invalid(c)
	case errors.Is(err, actressnamesync.ErrReloadBusy):
		c.Header("Retry-After", "5")
		fail(c, http.StatusTooManyRequests, "SYNC_RELOAD_BUSY", "offline evidence reload is already running")
	case errors.Is(err, actressnamesync.ErrReloadFailed):
		fail(c, http.StatusServiceUnavailable, "SYNC_RELOAD_FAILED", "offline evidence reload failed; current evidence was preserved")
	case errors.Is(err, actressnamesync.ErrStaleGeneration):
		fail(c, http.StatusConflict, "SYNC_STALE_GENERATION", "offline evidence changed; reload sources and request a new review")
	case errors.Is(err, actressnamesync.ErrDisabled):
		fail(c, http.StatusServiceUnavailable, "SYNC_DISABLED", "offline name sync is disabled")
	case errors.Is(err, actressnamesync.ErrUnavailable):
		unavailable(c)
	case errors.Is(err, actressnamesync.ErrStopping), errors.Is(err, context.Canceled):
		fail(c, http.StatusServiceUnavailable, "SYNC_STOPPING", "offline name sync request was cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		fail(c, http.StatusGatewayTimeout, "SYNC_TIMEOUT", "offline name sync request timed out")
	case errors.Is(err, actressnamesync.ErrReviewExpired):
		fail(c, http.StatusGone, "SYNC_REVIEW_EXPIRED", "review is unavailable or expired; request a new review")
	case errors.Is(err, actressnamesync.ErrPrincipal):
		fail(c, http.StatusForbidden, "SYNC_REVIEW_PRINCIPAL", "review is not available to this principal")
	case errors.Is(err, actressnamesync.ErrReviewCapacity), errors.Is(err, database.ErrActressSyncQueueFull):
		c.Header("Retry-After", "5")
		fail(c, http.StatusTooManyRequests, "SYNC_CAPACITY", "offline name sync capacity is temporarily exhausted")
	case errors.Is(err, database.ErrActressSyncProtected):
		fail(c, http.StatusConflict, "SYNC_PROTECTED", "selected actress is protected or unavailable")
	case errors.Is(err, database.ErrActressSyncStale):
		fail(c, http.StatusConflict, "SYNC_STALE", "review or evidence is stale; request a new review")
	case errors.Is(err, database.ErrActressSyncConflict), errors.Is(err, database.ErrActressSyncLease):
		fail(c, http.StatusConflict, "SYNC_CONFLICT", "offline name sync state conflicts with this request")
	case errors.Is(err, gorm.ErrRecordNotFound):
		fail(c, http.StatusNotFound, "SYNC_NOT_FOUND", "offline name sync record not found")
	default:
		fail(c, http.StatusInternalServerError, "SYNC_INTERNAL", "offline name sync request failed")
	}
}
