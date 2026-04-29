package worker

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

func saveScrapedResult(
	job *BatchJob,
	fileIndex int,
	filePath string,
	movie *models.Movie,
	movieID string,
	resolvedID string,
	results []*models.ScraperResult,
	usingCustomScrapers bool,
	movieRepo *database.MovieRepository,
	fieldSources map[string]string,
	actressSources map[string]string,
	posterErr *string,
	translationWarning string,
	matchResultPtr *matcher.MatchResult,
	startTime time.Time,
) (*models.Movie, *FileResult, error) {
	var finalMovie *models.Movie
	if !usingCustomScrapers {
		if movie.ID == "" && movie.ContentID == "" {
			logging.Warnf("[Batch %s] File %d: Critical - aggregated movie has empty ID and ContentID (resolvedID=%q, movieID=%q, scraperResults=%d)",
				job.ID, fileIndex, resolvedID, movieID, len(results))
			for i, r := range results {
				logging.Debugf("[Batch %s] File %d: Result[%d] source=%s, ID=%q, ContentID=%q, Title=%q",
					job.ID, fileIndex, i, r.Source, r.ID, r.ContentID, r.Title)
			}
		}
		if movie.Title == "" {
			logging.Warnf("[Batch %s] File %d: Aggregated movie has empty Title (resolvedID=%q, movieID=%q, scraperResults=%d)",
				job.ID, fileIndex, resolvedID, movieID, len(results))
			for i, r := range results {
				logging.Debugf("[Batch %s] File %d: Result[%d] source=%s, Title=%q, Maker=%q, Genres=%d",
					job.ID, fileIndex, i, r.Source, r.Title, r.Maker, len(r.Genres))
			}
		}
		logging.Debugf("[Batch %s] File %d: Saving metadata to database", job.ID, fileIndex)

		tempPosterURL := movie.CroppedPosterURL
		movie.CroppedPosterURL = ""

		savedMovie, err := movieRepo.Upsert(movie)
		if err != nil {
			return nil, nil, fmt.Errorf("database save failed for %s: %w", movie.ID, err)
		} else {
			logging.Debugf("[Batch %s] File %d: Successfully saved to database", job.ID, fileIndex)
		}

		movie.CroppedPosterURL = tempPosterURL

		if savedMovie != nil {
			finalMovie = savedMovie
			if movie.DisplayTitle != "" {
				finalMovie.DisplayTitle = movie.DisplayTitle
			}
			finalMovie.CroppedPosterURL = movie.CroppedPosterURL
			logging.Debugf("[Batch %s] File %d: Using saved movie from Upsert with associations", job.ID, fileIndex)
		} else {
			reloadedMovie, reloadErr := movieRepo.FindByID(movie.ID)
			if reloadErr == nil {
				finalMovie = reloadedMovie
				if movie.DisplayTitle != "" {
					finalMovie.DisplayTitle = movie.DisplayTitle
				}
				finalMovie.CroppedPosterURL = movie.CroppedPosterURL
			} else {
				logging.Debugf("[Batch %s] File %d: Failed to reload movie from database: %v", job.ID, fileIndex, reloadErr)
				finalMovie = movie
			}
		}
	} else {
		finalMovie = movie
	}

	now := time.Now()
	fileResult := &FileResult{
		FilePath:       filePath,
		MovieID:        movieID,
		Status:         JobStatusCompleted,
		Data:           finalMovie,
		FieldSources:   fieldSources,
		ActressSources: actressSources,
		PosterError:    posterErr,
		StartedAt:      startTime,
		EndedAt:        &now,
	}

	if translationWarning != "" {
		fileResult.TranslationWarning = &translationWarning
	}

	if matchResultPtr != nil {
		fileResult.IsMultiPart = matchResultPtr.IsMultiPart
		fileResult.PartNumber = matchResultPtr.PartNumber
		fileResult.PartSuffix = matchResultPtr.PartSuffix
	}

	logging.Debugf("[Batch %s] File %d: Scrape completed successfully", job.ID, fileIndex)

	return finalMovie, fileResult, nil
}

func newFailedFileResult(filePath string, movieID string, errMsg string, startTime time.Time) *FileResult {
	now := time.Now()
	return &FileResult{
		FilePath:  filePath,
		MovieID:   movieID,
		Status:    JobStatusFailed,
		Error:     errMsg,
		StartedAt: startTime,
		EndedAt:   &now,
	}
}

func newCancelledFileResult(filePath string, movieID string, startTime time.Time) *FileResult {
	now := time.Now()
	return &FileResult{
		FilePath:  filePath,
		MovieID:   movieID,
		Status:    JobStatusCancelled,
		Error:     "Cancelled by user",
		StartedAt: startTime,
		EndedAt:   &now,
	}
}

func newAggregationFailedResult(filePath string, movieID string, errMsg string, translationWarning string, startTime time.Time) *FileResult {
	fr := newFailedFileResult(filePath, movieID, errMsg, startTime)
	if translationWarning != "" {
		fr.TranslationWarning = &translationWarning
	}
	return fr
}
