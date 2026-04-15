package batch

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/worker"
)

func validateRescrapeRequest(req *BatchRescrapeRequest) (int, string) {
	if req.ManualSearchInput != "" {
		req.ManualSearchInput = strings.Map(func(r rune) rune {
			if r == '\u200B' || r == '\u200C' || r == '\u200D' || r == '\uFEFF' {
				return -1
			}
			return r
		}, req.ManualSearchInput)

		req.ManualSearchInput = strings.TrimSpace(req.ManualSearchInput)

		if req.ManualSearchInput == "" {
			return http.StatusBadRequest, "Manual search input cannot be empty"
		}
	}

	if req.Preset != "" {
		var presetErr error
		req.ScalarStrategy, req.ArrayStrategy, presetErr = nfo.ApplyPreset(req.Preset, req.ScalarStrategy, req.ArrayStrategy)
		if presetErr != nil {
			return http.StatusBadRequest, presetErr.Error()
		}
		logging.Infof("Applied preset '%s': scalar=%s, array=%s", req.Preset, req.ScalarStrategy, req.ArrayStrategy)
	}

	if len(req.SelectedScrapers) == 0 && req.ManualSearchInput == "" {
		return http.StatusBadRequest, "either selected_scrapers or manual_search_input must be provided"
	}

	return 0, ""
}

func validateJobState(job *worker.BatchJob) (isGone bool, httpStatus int, errMsg string) {
	job.Lock()

	if job.IsDeleted() {
		job.Unlock()
		return true, http.StatusGone, "Job has been deleted"
	}

	currentStatus := job.Status
	if currentStatus == worker.JobStatusRunning ||
		currentStatus == worker.JobStatusOrganized ||
		currentStatus == worker.JobStatusFailed ||
		currentStatus == worker.JobStatusCancelled {
		job.Unlock()
		return false, http.StatusConflict, fmt.Sprintf("Cannot rescrape %s job", currentStatus)
	}

	job.Unlock()
	return false, 0, ""
}

type fileLookupResult struct {
	foundFilePath    string
	oldMovieID       string
	capturedRevision uint64
}

func findFileForMovieID(job *worker.BatchJob, movieID string) (*fileLookupResult, int, string) {
	status := job.GetStatus()

	var matchingFiles []string
	for filePath, result := range status.Results {
		if result == nil {
			continue
		}
		if result.MovieID == movieID {
			matchingFiles = append(matchingFiles, filePath)
		}
	}

	if len(matchingFiles) == 0 {
		return nil, http.StatusNotFound, fmt.Sprintf("Movie %s not found in batch job", movieID)
	}

	if len(matchingFiles) > 1 {
		sort.Slice(matchingFiles, func(i, j int) bool {
			fi := status.FileMatchInfo[matchingFiles[i]]
			fj := status.FileMatchInfo[matchingFiles[j]]

			if fi.PartNumber != fj.PartNumber {
				if fi.PartNumber == 0 {
					return false
				}
				if fj.PartNumber == 0 {
					return true
				}
				return fi.PartNumber < fj.PartNumber
			}

			si := suffixOrder(fi.PartSuffix)
			sj := suffixOrder(fj.PartSuffix)
			if si != sj {
				return si < sj
			}

			return matchingFiles[i] < matchingFiles[j]
		})

		logging.Infof("[Rescrape] Multiple files found for movieID %s, selected %s from %v", movieID, matchingFiles[0], matchingFiles)
	}

	foundFilePath := matchingFiles[0]
	var capturedRevision uint64
	var oldMovieID string

	if result, ok := status.Results[foundFilePath]; ok && result != nil {
		capturedRevision = result.Revision

		if result.Data != nil {
			if oldMovie, ok := result.Data.(*models.Movie); ok {
				oldMovieID = oldMovie.ID
			}
		}
		if oldMovieID == "" {
			oldMovieID = result.MovieID
		}
	}

	if foundFilePath == "" {
		return nil, http.StatusNotFound, fmt.Sprintf("Movie %s not found in batch job", movieID)
	}

	return &fileLookupResult{
		foundFilePath:    foundFilePath,
		oldMovieID:       oldMovieID,
		capturedRevision: capturedRevision,
	}, 0, ""
}

func writeErrorResponse(c *gin.Context, status int, isGone bool, errMsg string) {
	if isGone {
		c.JSON(status, gin.H{
			"error":   errMsg,
			"skipped": true,
		})
		return
	}
	c.JSON(status, ErrorResponse{Error: errMsg})
}
