package actresssync

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	nameplan "github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/database"
)

// execute admits only the actual service operation, after body parsing. A slow
// client's unfinished body cannot hold the server's database shutdown join.
// Every DB-bearing request is cancelled by either HTTP or server shutdown and
// remains tracked until its synchronous service operation has returned.
func (h *handler) execute(c *gin.Context, status int, operation func(context.Context, syncService) (any, error)) {
	result, err := h.call(c.Request.Context(), operation)
	// Service outputs own their values. Release the database join before
	// serialization or network writes: a stalled reader must not pin shutdown.
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(status, result)
}

func (h *handler) call(requestCtx context.Context, operation func(context.Context, syncService) (any, error)) (any, error) {
	if h.lifetime == nil || h.service == nil {
		return nil, actressnamesync.ErrUnavailable
	}
	done := h.lifetime.TrackServerTask()
	if done == nil {
		return nil, actressnamesync.ErrStopping
	}
	defer done()
	ctx, cancel := context.WithCancel(requestCtx)
	stop := context.AfterFunc(h.lifetime.ServerCtx(), cancel)
	defer stop()
	defer cancel()
	if h.lifetime.ServerCtx().Err() != nil {
		cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service := h.service()
	if service == nil {
		return nil, actressnamesync.ErrUnavailable
	}
	return operation(ctx, service)
}

func (h *handler) status(c *gin.Context) {
	if _, ok := query(c); !ok {
		return
	}
	h.execute(c, http.StatusOK, func(ctx context.Context, svc syncService) (any, error) { return svc.Info(ctx) })
}

func (h *handler) sources(c *gin.Context) {
	values, ok := query(c, "q", "offset", "limit", "artifact_digest")
	if !ok {
		return
	}
	if len(values.Get("q")) > 128 || (values.Get("artifact_digest") != "" && !validHexID(values.Get("artifact_digest"))) {
		invalid(c)
		return
	}
	offset, limit, ok := page(c, values, 50, 100)
	if !ok {
		return
	}
	h.execute(c, http.StatusOK, func(ctx context.Context, svc syncService) (any, error) {
		return svc.SourcesAt(ctx, values.Get("q"), offset, limit, values.Get("artifact_digest"))
	})
}

func (h *handler) jobs(c *gin.Context) {
	values, ok := query(c, "cursor", "limit")
	if !ok {
		return
	}
	_, limit, ok := page(c, values, database.DefaultActressSyncHistoryJobs, database.MaxActressSyncHistoryJobs)
	if !ok {
		return
	}
	cursor := values.Get("cursor")
	if cursor != "" && !validJobID(cursor) {
		invalid(c)
		return
	}
	h.execute(c, http.StatusOK, func(ctx context.Context, svc syncService) (any, error) { return svc.ListJobs(ctx, cursor, limit) })
}

func (h *handler) job(c *gin.Context) {
	if _, ok := query(c); !ok {
		return
	}
	id := c.Param("id")
	if !validJobID(id) {
		invalid(c)
		return
	}
	h.execute(c, http.StatusOK, func(ctx context.Context, svc syncService) (any, error) { return svc.Job(ctx, id) })
}

func (h *handler) tasks(c *gin.Context) {
	values, ok := query(c, "offset", "limit")
	if !ok {
		return
	}
	offset, limit, ok := page(c, values, 50, 100)
	if !ok {
		return
	}
	id := c.Param("id")
	if !validJobID(id) {
		invalid(c)
		return
	}
	h.execute(c, http.StatusOK, func(ctx context.Context, svc syncService) (any, error) { return svc.Tasks(ctx, id, offset, limit) })
}

type choiceRequest struct {
	ActressID uint   `json:"actress_id"`
	SourceKey string `json:"source_key"`
}

type reviewRequest struct {
	Choices        []choiceRequest `json:"choices"`
	ArtifactDigest string          `json:"artifact_digest,omitempty"`
}

func (h *handler) review(c *gin.Context) {
	if _, ok := query(c); !ok {
		return
	}
	var request reviewRequest
	if !decodeJSON(c, &request) {
		return
	}
	if len(request.Choices) == 0 || len(request.Choices) > nameplan.MaxSelection || (request.ArtifactDigest != "" && !validHexID(request.ArtifactDigest)) {
		invalid(c)
		return
	}
	choices := make([]database.ActressSyncChoice, 0, len(request.Choices))
	ids, sources := map[uint]bool{}, map[string]bool{}
	for _, choice := range request.Choices {
		if choice.ActressID == 0 || ids[choice.ActressID] || sources[choice.SourceKey] || len(choice.SourceKey) == 0 || len(choice.SourceKey) > 512 || !utf8.ValidString(choice.SourceKey) || strings.ContainsAny(choice.SourceKey, "\x00\r\n") {
			invalid(c)
			return
		}
		ids[choice.ActressID], sources[choice.SourceKey] = true, true
		choices = append(choices, database.ActressSyncChoice{ActressID: choice.ActressID, SourceKey: choice.SourceKey})
	}
	h.execute(c, http.StatusCreated, func(ctx context.Context, svc syncService) (any, error) {
		return svc.ReviewAt(ctx, principal(c), choices, request.ArtifactDigest)
	})
}

type confirmRequest struct {
	ReviewID  string `json:"review_id"`
	Confirmed bool   `json:"confirmed"`
}

func (h *handler) confirm(c *gin.Context) {
	if _, ok := query(c); !ok {
		return
	}
	var request confirmRequest
	if !decodeJSON(c, &request) {
		return
	}
	if !request.Confirmed || !validHexID(request.ReviewID) {
		invalid(c)
		return
	}
	h.execute(c, http.StatusAccepted, func(ctx context.Context, svc syncService) (any, error) {
		return svc.Confirm(ctx, principal(c), request.ReviewID, true)
	})
}

func (h *handler) cancel(c *gin.Context) {
	if _, ok := query(c); !ok {
		return
	}
	var request struct {
		Confirmed bool `json:"confirmed"`
	}
	if !decodeJSON(c, &request) {
		return
	}
	id := c.Param("id")
	if !request.Confirmed || !validJobID(id) {
		invalid(c)
		return
	}
	h.execute(c, http.StatusOK, func(ctx context.Context, svc syncService) (any, error) { return svc.Cancel(ctx, id) })
}

// reload accepts no artifact authority; the service uses restart-pinned settings.
func (h *handler) reload(c *gin.Context) {
	if _, ok := query(c); !ok {
		return
	}
	var request struct{}
	if !decodeJSONBody(c, &request, true) {
		return
	}
	h.execute(c, http.StatusOK, func(ctx context.Context, svc syncService) (any, error) { return svc.Reload(ctx) })
}
