package worker

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

func generateScrapedPoster(
	ctx context.Context,
	job *BatchJob,
	fileIndex int,
	movie *models.Movie,
	httpClient httpclientiface.HTTPClient,
	userAgent string,
	referer string,
	processedMovieIDs map[string]bool,
	cfg *config.Config,
) *string {
	if httpClient == nil {
		return nil
	}

	shouldGeneratePoster := true

	if processedMovieIDs != nil {
		processedMovieIDsMutex.Lock()
		if processedMovieIDs[movie.ID] {
			shouldGeneratePoster = false
			logging.Debugf("[Batch %s] File %d: Skipping poster generation for %s (already processed for multi-part file)",
				job.ID, fileIndex, movie.ID)
		} else {
			processedMovieIDs[movie.ID] = true
		}
		processedMovieIDsMutex.Unlock()
	}

	if shouldGeneratePoster {
		tempPosterURL, err := GenerateTempPoster(ctx, job.ID, movie, httpClient, userAgent, referer, downloader.ResolveMediaReferer, cfg.System.TempDir)
		if err != nil {
			logging.Warnf("[Batch %s] File %d: Failed to create temp poster: %v (continuing anyway)", job.ID, fileIndex, err)
			errMsg := err.Error()
			return &errMsg
		}
		movie.CroppedPosterURL = tempPosterURL
	} else {
		movie.CroppedPosterURL = fmt.Sprintf("/api/v1/temp/posters/%s/%s.jpg", job.ID, movie.ID)
	}

	return nil
}
