package history

import (
	"encoding/json"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Logger handles logging operations to the history database
type Logger struct {
	repo *database.HistoryRepository
}

// NewLogger creates a new history logger
func NewLogger(db *database.DB) *Logger {
	return &Logger{
		repo: database.NewHistoryRepository(db),
	}
}

// LogOrganize logs a file organization operation
func (l *Logger) LogOrganize(movieID, originalPath, newPath string, dryRun bool, err error) error {
	status := "success"
	errorMsg := ""
	if err != nil {
		status = "failed"
		errorMsg = err.Error()
	}

	history := &models.History{
		MovieID:      movieID,
		Operation:    "organize",
		OriginalPath: originalPath,
		NewPath:      newPath,
		Status:       status,
		ErrorMessage: errorMsg,
		DryRun:       dryRun,
		CreatedAt:    time.Now().UTC(),
	}

	return l.repo.Create(history)
}

// LogScrape logs a metadata scraping operation
func (l *Logger) LogScrape(movieID, sourceURL string, metadata interface{}, err error) error {
	status := "success"
	errorMsg := ""
	if err != nil {
		status = "failed"
		errorMsg = err.Error()
	}

	// Convert metadata to JSON
	metadataJSON := ""
	if metadata != nil {
		if bytes, jsonErr := json.Marshal(metadata); jsonErr == nil {
			metadataJSON = string(bytes)
		}
	}

	history := &models.History{
		MovieID:      movieID,
		Operation:    "scrape",
		OriginalPath: sourceURL,
		NewPath:      "",
		Status:       status,
		ErrorMessage: errorMsg,
		Metadata:     metadataJSON,
		DryRun:       false,
		CreatedAt:    time.Now().UTC(),
	}

	return l.repo.Create(history)
}

// LogDownload logs a media download operation
func (l *Logger) LogDownload(movieID, url, localPath, mediaType string, err error) error {
	status := "success"
	errorMsg := ""
	if err != nil {
		status = "failed"
		errorMsg = err.Error()
	}

	// Store media type in metadata
	metadataMap := map[string]string{
		"media_type": mediaType,
	}
	metadataJSON, _ := json.Marshal(metadataMap)

	history := &models.History{
		MovieID:      movieID,
		Operation:    "download",
		OriginalPath: url,
		NewPath:      localPath,
		Status:       status,
		ErrorMessage: errorMsg,
		Metadata:     string(metadataJSON),
		DryRun:       false,
		CreatedAt:    time.Now().UTC(),
	}

	return l.repo.Create(history)
}

// LogNFO logs an NFO generation operation
func (l *Logger) LogNFO(movieID, nfoPath string, err error) error {
	status := "success"
	errorMsg := ""
	if err != nil {
		status = "failed"
		errorMsg = err.Error()
	}

	history := &models.History{
		MovieID:      movieID,
		Operation:    "nfo",
		OriginalPath: "",
		NewPath:      nfoPath,
		Status:       status,
		ErrorMessage: errorMsg,
		DryRun:       false,
		CreatedAt:    time.Now().UTC(),
	}

	return l.repo.Create(history)
}

// LogRevert logs a revert operation
func (l *Logger) LogRevert(movieID, originalPath, revertedFrom string, err error) error {
	status := "reverted"
	errorMsg := ""
	if err != nil {
		status = "failed"
		errorMsg = err.Error()
	}

	history := &models.History{
		MovieID:      movieID,
		Operation:    "organize", // Revert is still an organize operation
		OriginalPath: revertedFrom,
		NewPath:      originalPath,
		Status:       status,
		ErrorMessage: errorMsg,
		DryRun:       false,
		CreatedAt:    time.Now().UTC(),
	}

	return l.repo.Create(history)
}

// GetRecent retrieves recent history records
func (l *Logger) GetRecent(limit int) ([]models.History, error) {
	return l.repo.FindRecent(limit)
}

// GetByMovieID retrieves history for a specific movie
func (l *Logger) GetByMovieID(movieID string) ([]models.History, error) {
	return l.repo.FindByMovieID(movieID)
}

// GetByOperation retrieves history for a specific operation type
func (l *Logger) GetByOperation(operation string, limit int) ([]models.History, error) {
	return l.repo.FindByOperation(operation, limit)
}

// GetByStatus retrieves history with a specific status
func (l *Logger) GetByStatus(status string, limit int) ([]models.History, error) {
	return l.repo.FindByStatus(status, limit)
}

// GetStats returns statistics about operations
func (l *Logger) GetStats() (*Stats, error) {
	stats := &Stats{}

	// Get total count
	total, err := l.repo.Count()
	if err != nil {
		return nil, err
	}
	stats.Total = total

	// Get counts by status
	success, err := l.repo.CountByStatus("success")
	if err != nil {
		return nil, err
	}
	stats.Success = success

	failed, err := l.repo.CountByStatus("failed")
	if err != nil {
		return nil, err
	}
	stats.Failed = failed

	reverted, err := l.repo.CountByStatus("reverted")
	if err != nil {
		return nil, err
	}
	stats.Reverted = reverted

	// Get counts by operation
	scrapeCount, err := l.repo.CountByOperation("scrape")
	if err != nil {
		return nil, err
	}
	stats.Scrape = scrapeCount

	organizeCount, err := l.repo.CountByOperation("organize")
	if err != nil {
		return nil, err
	}
	stats.Organize = organizeCount

	downloadCount, err := l.repo.CountByOperation("download")
	if err != nil {
		return nil, err
	}
	stats.Download = downloadCount

	nfoCount, err := l.repo.CountByOperation("nfo")
	if err != nil {
		return nil, err
	}
	stats.NFO = nfoCount

	return stats, nil
}

// Stats represents history statistics
type Stats struct {
	Total    int64
	Success  int64
	Failed   int64
	Reverted int64
	Scrape   int64
	Organize int64
	Download int64
	NFO      int64
}

// CleanupOldRecords removes history records older than the specified duration
func (l *Logger) CleanupOldRecords(olderThan time.Duration) error {
	cutoffDate := time.Now().UTC().Add(-olderThan)
	return l.repo.DeleteOlderThan(cutoffDate)
}
