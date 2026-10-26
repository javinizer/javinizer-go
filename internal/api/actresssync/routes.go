// Package actresssync exposes explicitly reviewed offline Japanese-name jobs.
// It is separate from actress CRUD and cannot accept client-authored patches.
package actresssync

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
)

const routePrefix = "/actress-name-sync"

// syncService deliberately excludes repository, worker, artifact and clock access.
type syncService interface {
	Info(context.Context) (actressnamesync.SourceInfo, error)
	Reload(context.Context) (actressnamesync.SourceInfo, error)
	SourcesAt(context.Context, string, int, int, string) (*actressnamesync.SourcePage, error)
	ReviewAt(context.Context, string, []database.ActressSyncChoice, string) (*actressnamesync.Review, error)
	Confirm(context.Context, string, string, bool) (*actressnamesync.JobView, error)
	Job(context.Context, string) (*actressnamesync.JobView, error)
	Tasks(context.Context, string, int, int) (*actressnamesync.TaskPage, error)
	ListJobs(context.Context, string, int) (*actressnamesync.JobPage, error)
	Cancel(context.Context, string) (*actressnamesync.JobView, error)
}

type serverLifetime interface {
	TrackServerTask() func()
	ServerCtx() context.Context
}

type handler struct {
	service  func() syncService
	lifetime serverLifetime
	origins  func() []string
}

// RegisterRoutes attaches reads to the existing authenticated group and writes
// to its rate-limited child. It does not replace or relax global authentication.
func RegisterRoutes(protected, writeProtected *gin.RouterGroup, rt *core.APIRuntime) {
	h := &handler{}
	if rt != nil {
		h.lifetime = rt
		h.service = func() syncService {
			service := rt.OfflineNameSync()
			if service == nil {
				return nil
			}
			return service
		}
		h.origins = func() []string { return rt.GetAPIConfig().AllowedOrigins }
	}
	h.register(protected, writeProtected)
}

func (h *handler) register(protected, writeProtected *gin.RouterGroup) {
	reads := protected.Group(routePrefix, h.authenticated)
	reads.GET("/status", h.status)
	reads.GET("/sources", h.sources)
	reads.GET("/jobs", h.jobs)
	reads.GET("/jobs/:id", h.job)
	reads.GET("/jobs/:id/tasks", h.tasks)

	writes := writeProtected.Group(routePrefix, h.authenticated, h.writeIntent)
	writes.POST("/sources/reload", h.reload)
	writes.POST("/reviews", h.review)
	writes.POST("/jobs", h.confirm)
	writes.POST("/jobs/:id/cancel", h.cancel)
}
