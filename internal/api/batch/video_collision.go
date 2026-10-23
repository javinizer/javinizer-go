package batch

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

const (
	videoCollisionKindNew      = "new"
	videoCollisionKindSameFile = "same-file"
	videoCollisionKindStale    = "stale"
	videoCollisionKindFile     = "file"
)

type videoCollisionReport = contracts.VideoCollisionReviewResponse

func reviewVideoCollisions(ctx context.Context, snap *core.RuntimeSnapshot, job worker.BatchJobInterface, cfg worker.ApplyPhaseConfig) (*videoCollisionReport, error) {
	report := &videoCollisionReport{Items: []videocollision.Collision{}}
	if cfg.OrganizeOptions.Skip {
		return report, nil
	}
	wf, err := snap.BatchWorkflow(job.GetID())
	if err != nil {
		return nil, err
	}
	status := job.GetStatus()
	paths := make([]string, 0, len(status.Results))
	for source := range cfg.OrganizeOptions.VideoCollisionDecisions {
		if _, ok := status.Results[source]; !ok {
			return nil, fmt.Errorf("video decision source is not in this job: %s", source)
		}
	}
	retry := map[string]bool{}
	for _, path := range cfg.RetryFilePaths {
		retry[path] = true
	}
	for path, result := range status.Results {
		if result == nil || result.Movie == nil || status.Excluded[path] {
			continue
		}
		if len(retry) > 0 && !retry[path] {
			continue
		}
		if result.Status != models.JobStatusCompleted && result.Status != models.JobStatusFailed {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	claimed := map[string]string{}
	for _, path := range paths {
		result := status.Results[path]
		_, hasDecision := cfg.OrganizeOptions.VideoCollisionDecisions[filepath.Clean(path)]
		if _, err := snap.RT().Deps().GetFs().Stat(path); os.IsNotExist(err) && !hasDecision && cfg.OrganizeOptions.VideoCollisionPolicy == videocollision.Fail {
			continue
		}
		movie, authorityErr := authoritativePreviewMovie(ctx, snap.RT().Deps(), job.GetID(), result.ResultID, nil)
		if authorityErr != nil {
			return nil, authorityErr
		}
		preview, err := wf.Preview(ctx, workflow.PreviewCmd{Movie: movie, FileResults: []models.FileMatchInfo{result.FileMatchInfo}, Destination: cfg.Destination, OperationMode: cfg.OperationModeOverride, ForceRenameFile: cfg.OrganizeOptions.ForceRenameFile, VideoCollisionScope: job.GetID(), VideoCollisionSelectedSources: cfg.OrganizeOptions.VideoCollisionSelectedSources, VideoCollisionGeneration: cfg.OrganizeOptions.VideoCollisionGeneration, SkipNFO: !cfg.GenerateNFO, SkipDownload: !cfg.Download})
		if err != nil {
			return nil, err
		}
		if preview == nil || preview.FullPath == "" {
			return nil, fmt.Errorf("cannot resolve planned video destination for %s", path)
		}
		item := videocollision.Collision{Source: path, Destination: preview.FullPath, Kind: videoCollisionKindNew}
		if source, err := snap.RT().Deps().GetFs().Stat(path); err == nil {
			item.SourceSize = source.Size()
		}
		if len(preview.VideoCollisions) > 0 {
			for _, collision := range preview.VideoCollisions {
				if collision.Source == path && collision.Destination == preview.FullPath {
					item = collision
					break
				}
			}
		}
		if item.Kind == videoCollisionKindNew {
			if _, err := snap.RT().Deps().GetFs().Stat(item.Destination); err == nil {
				item.Kind = videoCollisionKindSameFile
			}
		}
		decision, explicit := cfg.OrganizeOptions.VideoCollisionDecisions[filepath.Clean(path)]
		policy := cfg.OrganizeOptions.VideoCollisionPolicy
		if explicit {
			policy = decision.Policy
		}
		if explicit && (filepath.Clean(decision.Source) != filepath.Clean(path) || filepath.Clean(decision.Destination) != filepath.Clean(item.Destination)) {
			item.Kind = videoCollisionKindStale
		}
		hasSiblingConflict := false
		for _, collision := range preview.VideoCollisions {
			if (collision.Source != path || collision.Destination != preview.FullPath) && collision.Kind != videoCollisionKindNew && collision.Kind != videoCollisionKindSameFile {
				hasSiblingConflict = true
				break
			}
		}
		if policy == videocollision.Skip && item.Kind != videoCollisionKindStale && (explicit || item.Kind != videoCollisionKindNew && item.Kind != videoCollisionKindSameFile || hasSiblingConflict) && (decision.ReviewToken == "" || decision.ReviewToken == item.ReviewToken) {
			report.Skipped++
			report.Items = append(report.Items, item)
			continue
		}
		for _, collision := range preview.VideoCollisions {
			if collision.Source == path && collision.Destination == preview.FullPath {
				continue
			}
			collision.SelectedSource = path
			key := fsutil.DestKey(collision.Destination)
			if previous := claimed[key]; previous != "" && previous != collision.Source {
				collision.Kind = "duplicate"
			} else {
				claimed[key] = collision.Source
			}
			switch collision.Kind {
			case videoCollisionKindNew:
				report.New++
			case videoCollisionKindSameFile:
				report.SameFile++
			default:
				report.Unresolved++
			}
			report.Items = append(report.Items, collision)
		}
		key := fsutil.DestKey(item.Destination)
		if previous := claimed[key]; previous != "" && previous != path {
			item.Kind = "duplicate"
		} else {
			claimed[key] = path
		}
		switch {
		case explicit && policy == videocollision.Replace && (item.Kind != videoCollisionKindFile || item.ReviewToken == "" || decision.ReviewToken != item.ReviewToken):
			item.Kind = videoCollisionKindStale
			report.Unresolved++
		case item.Kind == videoCollisionKindNew:
			report.New++
		case item.Kind == videoCollisionKindSameFile:
			report.SameFile++
		case item.Kind == videoCollisionKindFile && explicit && policy == videocollision.Replace && decision.ReviewToken == item.ReviewToken:
			report.Replaced++
		default:
			report.Unresolved++
		}
		report.Items = append(report.Items, item)
	}
	return report, nil
}

// @Summary Review planned video destinations
// @Description Read-only collision preflight with source/destination sizes and server-issued snapshot-bound review receipts.
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param request body contracts.OrganizeRequest true "Organization settings and existing review decisions"
// @Success 200 {object} contracts.VideoCollisionReviewResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/video-collisions [post]
func videoCollisionReview(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.OrganizeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		snap := rt.Snapshot()
		job, ok := rt.Deps().GetJobStore().GetBatchJob(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: jobNotFoundMessage})
			return
		}
		factory := snap.BatchJobFactory()
		if factory == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: factoryUnavailableMessage})
			return
		}
		cfg, err := resolveOrganizeApplyConfig(snap, factory, job, req)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		report, err := reviewVideoCollisions(c.Request.Context(), snap, job, cfg)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, report)
	}
}
