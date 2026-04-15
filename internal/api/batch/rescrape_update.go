package batch

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

type updateResult struct {
	shouldAbort  bool
	posterPaths  []string
	httpStatus   int
	errorMessage string
	isGone       bool
}

func validateAndUpdateResult(
	job *worker.BatchJob,
	result *worker.FileResult,
	foundFilePath string,
	capturedRevision uint64,
	movie *models.Movie,
	oldMovieID string,
	cfg *config.Config,
	jobID string,
) updateResult {
	job.Lock()
	if job.IsDeleted() {
		job.Unlock()
		logging.Infof("[Rescrape] Job %s was deleted during rescrape, discarding results", jobID)
		cleanupMoviePosters(cfg, jobID, movie)
		return updateResult{shouldAbort: true, httpStatus: http.StatusGone, errorMessage: "Job was deleted during rescrape", isGone: true}
	}

	if isJobTransitioned(job, jobID) {
		job.Unlock()
		cleanupMoviePosters(cfg, jobID, movie)
		return updateResult{shouldAbort: true, httpStatus: http.StatusConflict, errorMessage: fmt.Sprintf("Job transitioned to %s during rescrape, changes discarded", job.Status)}
	}

	applyMultipartMetadata(job, result, foundFilePath)

	currentMovieIDBeforeUpdate := extractCurrentMovieID(job, foundFilePath)

	if abort, orphaned := checkConcurrentModification(job, foundFilePath, capturedRevision, movie, cfg, jobID); abort {
		job.Unlock()
		cleanupPosterPaths(orphaned)
		return updateResult{shouldAbort: true, httpStatus: http.StatusConflict, errorMessage: "File was concurrently rescraped, discarding stale result", isGone: true}
	}

	result.Revision = capturedRevision + 1
	job.Results[foundFilePath] = result
	updateJobProgress(job)

	posterPathsToCleanup := collectOrphanedPosterPaths(job, foundFilePath, currentMovieIDBeforeUpdate, movie, oldMovieID, cfg, jobID)

	job.Unlock()
	return updateResult{shouldAbort: false, posterPaths: posterPathsToCleanup}
}

func isJobTransitioned(job *worker.BatchJob, jobID string) bool {
	s := job.Status
	return s == worker.JobStatusRunning || s == worker.JobStatusOrganized || s == worker.JobStatusFailed || s == worker.JobStatusCancelled
}

func cleanupMoviePosters(cfg *config.Config, jobID string, movie *models.Movie) {
	if movie != nil && movie.ID != "" {
		cleanupPosterPaths([]string{
			filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+".jpg"),
			filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+"-full.jpg"),
		})
	}
}

func applyMultipartMetadata(job *worker.BatchJob, result *worker.FileResult, foundFilePath string) {
	if info, ok := job.FileMatchInfo[foundFilePath]; ok {
		result.IsMultiPart = info.IsMultiPart
		result.PartNumber = info.PartNumber
		result.PartSuffix = info.PartSuffix
		logging.Debugf("[Rescrape] Applied discovery multipart metadata for %s: IsMultiPart=%v, PartNumber=%d",
			foundFilePath, info.IsMultiPart, info.PartNumber)
	}
}

func extractCurrentMovieID(job *worker.BatchJob, foundFilePath string) string {
	if existingResult := job.Results[foundFilePath]; existingResult != nil {
		if existingResult.Data != nil {
			if existingMovie, ok := existingResult.Data.(*models.Movie); ok {
				return existingMovie.ID
			}
		}
		if existingResult.MovieID != "" {
			return existingResult.MovieID
		}
	}
	return ""
}

func checkConcurrentModification(job *worker.BatchJob, foundFilePath string, capturedRevision uint64, movie *models.Movie, cfg *config.Config, jobID string) (bool, []string) {
	currentResult := job.Results[foundFilePath]
	currentRevision := uint64(0)
	if currentResult != nil {
		currentRevision = currentResult.Revision
	}
	if currentRevision == capturedRevision {
		return false, nil
	}

	shouldCleanup := shouldCleanupPosterOnConflict(currentResult, movie, cfg, jobID)
	var orphaned []string
	if shouldCleanup && movie != nil && movie.ID != "" {
		orphaned = append(orphaned,
			filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+".jpg"),
			filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+"-full.jpg"),
		)
	}
	return true, orphaned
}

func shouldCleanupPosterOnConflict(currentResult *worker.FileResult, movie *models.Movie, cfg *config.Config, jobID string) bool {
	if movie == nil || movie.ID == "" || currentResult == nil {
		return true
	}

	if movieIDMatches(currentResult.MovieID, movie.ID, cfg, jobID) {
		logging.Infof("[Rescrape] Skipping poster cleanup - winner has same movie.ID (%s)", movie.ID)
		return false
	}
	if currentResult.Data != nil {
		if winnerMovie, ok := currentResult.Data.(*models.Movie); ok {
			if movieIDMatches(winnerMovie.ID, movie.ID, cfg, jobID) {
				logging.Infof("[Rescrape] Skipping poster cleanup - winner has same canonical movie.ID (%s)", movie.ID)
				return false
			}
		}
	}
	return true
}

func movieIDMatches(id1, id2 string, cfg *config.Config, jobID string) bool {
	if id1 == id2 {
		return true
	}
	posterDir := filepath.Join(cfg.System.TempDir, "posters", jobID)
	if isCaseInsensitiveFSCached(posterDir) {
		return strings.EqualFold(id1, id2)
	}
	return false
}

func updateJobProgress(job *worker.BatchJob) {
	completed := 0
	failed := 0
	for _, r := range job.Results {
		if r == nil {
			continue
		}
		switch r.Status {
		case worker.JobStatusCompleted:
			completed++
		case worker.JobStatusFailed:
			failed++
		}
	}
	job.Completed = completed
	job.Failed = failed
	if job.TotalFiles == 0 {
		job.Progress = 100
	} else {
		job.Progress = float64(completed+failed) / float64(job.TotalFiles) * 100
	}
}

func collectOrphanedPosterPaths(job *worker.BatchJob, foundFilePath string, currentMovieIDBeforeUpdate string, movie *models.Movie, oldMovieID string, cfg *config.Config, jobID string) []string {
	var paths []string

	if currentMovieIDBeforeUpdate != "" && currentMovieIDBeforeUpdate != movie.ID {
		if !otherResultUsesMovieID(job, foundFilePath, currentMovieIDBeforeUpdate) {
			paths = appendConcurrentChangePosterPaths(paths, cfg, jobID, currentMovieIDBeforeUpdate, movie.ID)
		}
	}

	if movie != nil && movie.ID != "" && oldMovieID != "" && movie.ID != oldMovieID {
		if currentMovieIDBeforeUpdate == oldMovieID {
			paths = appendOldIDPosterPaths(paths, job, foundFilePath, movie, oldMovieID, cfg, jobID)
		}
	}

	return paths
}

func otherResultUsesMovieID(job *worker.BatchJob, excludePath string, movieID string) bool {
	for filePath, otherResult := range job.Results {
		if filePath == excludePath || otherResult == nil {
			continue
		}
		if strings.EqualFold(otherResult.MovieID, movieID) {
			return true
		}
		if otherResult.Data != nil {
			if otherMovie, ok := otherResult.Data.(*models.Movie); ok && strings.EqualFold(otherMovie.ID, movieID) {
				return true
			}
		}
	}
	return false
}

func appendConcurrentChangePosterPaths(paths []string, cfg *config.Config, jobID string, oldID string, newID string) []string {
	if strings.EqualFold(oldID, newID) {
		posterDir := filepath.Join(cfg.System.TempDir, "posters", jobID)
		if isCaseInsensitiveFSCached(posterDir) {
			logging.Infof("[Rescrape] Concurrent case change detected (%s → %s), skipping poster cleanup (case-insensitive filesystem)", oldID, newID)
			return paths
		}
		logging.Infof("[Rescrape] Concurrent case change detected (%s → %s) on case-sensitive filesystem, cleaning up poster", oldID, newID)
	} else {
		logging.Infof("[Rescrape] Concurrent modification detected, cleaning up poster for %s (overwritten)", oldID)
	}
	return append(paths,
		filepath.Join(cfg.System.TempDir, "posters", jobID, oldID+".jpg"),
		filepath.Join(cfg.System.TempDir, "posters", jobID, oldID+"-full.jpg"),
	)
}

func appendOldIDPosterPaths(paths []string, job *worker.BatchJob, foundFilePath string, movie *models.Movie, oldMovieID string, cfg *config.Config, jobID string) []string {
	if strings.EqualFold(movie.ID, oldMovieID) {
		posterDir := filepath.Join(cfg.System.TempDir, "posters", jobID)
		if isCaseInsensitiveFSCached(posterDir) {
			logging.Infof("[Rescrape] ID case change detected (%s → %s), skipping poster cleanup (case-insensitive filesystem)", oldMovieID, movie.ID)
			return paths
		}
		logging.Infof("[Rescrape] ID case change detected (%s → %s) on case-sensitive filesystem, will clean up old poster", oldMovieID, movie.ID)
	}

	if otherResultUsesMovieID(job, foundFilePath, oldMovieID) {
		logging.Debugf("[Rescrape] Skipping poster cleanup for %s - other result still uses this ID", oldMovieID)
		return paths
	}

	return append(paths,
		filepath.Join(cfg.System.TempDir, "posters", jobID, oldMovieID+".jpg"),
		filepath.Join(cfg.System.TempDir, "posters", jobID, oldMovieID+"-full.jpg"),
	)
}
