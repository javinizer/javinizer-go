package batch

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// mapBatchEditError is the single typed-error → HTTP mapping for batch edit
// operations (POSTER-WRITE-HARDENING D10): errors.Is-based, no substring
// matching. Returns true when err was a known typed error and the response
// was written; false means "unknown" — the caller maps it to 500.
//
//	worker.ErrJobNotFound / family-empty / not-found variants → 404
//	worker.ErrJobGone                                     → 410
//	worker.ErrEditNotAdmitted (incl. EditPhaseBusyError)  → 409
//	*worker.EditAdmissionConflictError (D17 identity)     → 409
func mapBatchEditError(c *gin.Context, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, worker.ErrJobGone):
		c.JSON(http.StatusGone, contracts.ErrorResponse{Error: err.Error()})
	case errors.Is(err, worker.ErrJobNotFound):
		// Legacy response wording parity (pre-hardening handlers wrote the
		// constant "Job not found" — UI affixes + tests key off it).
		c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
	case errors.Is(err, worker.ErrMovieFamilyEmpty):
		c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: err.Error()})
	case errors.Is(err, worker.ErrEditNotAdmitted),
		errors.Is(err, worker.ErrFamilyRekeyed):
		c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: err.Error()})
	default:
		var conflict *worker.EditAdmissionConflictError
		if errors.As(err, &conflict) {
			c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: err.Error()})
			return true
		}
		return false
	}
	return true
}

// admitOrWriteError acquires an admitted edit lease for jobID and returns the
// job + release func. On failure it writes the typed response and returns
// zero values; callers return early. Unknown (untyped) admission failures map
// to 500 so the response stream is never left unwritten.
func admitOrWriteError(c *gin.Context, acquire func(string) (worker.BatchJobInterface, func(), error)) (worker.BatchJobInterface, func(), bool) {
	jobID := c.Param("id")
	job, release, err := acquire(jobID)
	if err != nil {
		if !mapBatchEditError(c, err) {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
		}
		return nil, nil, false
	}
	return job, release, true
}

// currentResultRevision returns the post-commit result revision for the CAS
// echo (D12). Nil when the result is unresolvable — clients fall back to a
// fresh fetch, which is the original contract.
func currentResultRevision(job worker.BatchJobInterface, resultID string) *uint64 {
	res, _, ok := job.GetFileResultByResultID(resultID)
	if !ok || res == nil {
		return nil
	}
	rv := res.Revision
	return &rv
}

// familyRevisions returns fresh per-part revisions for the movie family that
// resultID belongs to, keyed by result_id (codex r26): each part publishes
// independently and its revision bump is per-part.
func familyRevisions(job worker.BatchJobInterface, resultID string) map[string]uint64 {
	_, filePaths, found := lookupResultByResultID(job, resultID)
	if !found {
		return nil
	}
	revs := make(map[string]uint64, len(filePaths))
	for _, fp := range filePaths {
		res, err := job.GetMovieResult(fp)
		if err != nil || res == nil {
			continue
		}
		if res.ResultID != "" {
			revs[res.ResultID] = res.Revision
		}
	}
	return revs
}

// writeEditOpError maps an in-operation typed error with a sensible fallback:
// typed (404/409/410) via mapBatchEditError, downloader/SSRF-flavored strings
// keep their historical 400/502 split, everything else 500.
func writeEditOpError(c *gin.Context, err error) {
	if mapBatchEditError(c, err) {
		return
	}
	if strings.Contains(err.Error(), "SSRF") || strings.Contains(err.Error(), "invalid URL") {
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
		return
	}
	if strings.Contains(err.Error(), "download") || strings.Contains(err.Error(), "status") {
		c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
}
