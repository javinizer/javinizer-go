package database

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// MovieRepositoryInterface defines the contract for movie database operations
type MovieRepositoryInterface interface {
	Create(movie *models.Movie) error
	Update(movie *models.Movie) error
	Upsert(movie *models.Movie) (*models.Movie, error)
	FindByID(id string) (*models.Movie, error)
	FindByContentID(contentID string) (*models.Movie, error)
	Delete(id string) error
	List(limit, offset int) ([]models.Movie, error)
}

// ActressRepositoryInterface defines the contract for actress database operations
type ActressRepositoryInterface interface {
	Create(actress *models.Actress) error
	Update(actress *models.Actress) error
	FindByJapaneseName(name string) (*models.Actress, error)
	FindOrCreate(actress *models.Actress) error
	List(limit, offset int) ([]models.Actress, error)
	Search(query string) ([]models.Actress, error)
}

// MovieTranslationRepositoryInterface defines the contract for movie translation operations
type MovieTranslationRepositoryInterface interface {
	Upsert(translation *models.MovieTranslation) error
	UpsertTx(tx *gorm.DB, translation *models.MovieTranslation) error
	FindByMovieAndLanguage(movieID, language string) (*models.MovieTranslation, error)
	FindAllByMovie(movieID string) ([]models.MovieTranslation, error)
	Delete(movieID, language string) error
}

// GenreReplacementRepositoryInterface defines the contract for genre replacement operations
type GenreReplacementRepositoryInterface interface {
	Create(replacement *models.GenreReplacement) error
	Upsert(replacement *models.GenreReplacement) error
	FindByOriginal(original string) (*models.GenreReplacement, error)
	List() ([]models.GenreReplacement, error)
	Delete(original string) error
	GetReplacementMap() (map[string]string, error)
}

// HistoryRepositoryInterface defines the contract for history tracking operations
type HistoryRepositoryInterface interface {
	Create(history *models.History) error
	FindByID(id uint) (*models.History, error)
	FindByMovieID(movieID string) ([]models.History, error)
	FindByBatchJobID(batchJobID string) ([]models.History, error)
	FindByOperation(operation string, limit int) ([]models.History, error)
	FindByStatus(status string, limit int) ([]models.History, error)
	FindRecent(limit int) ([]models.History, error)
	FindByDateRange(start, end time.Time) ([]models.History, error)
	Count() (int64, error)
	CountByStatus(status string) (int64, error)
	CountByOperation(operation string) (int64, error)
	Delete(id uint) error
	DeleteByMovieID(movieID string) error
	DeleteOlderThan(date time.Time) error
	List(limit, offset int) ([]models.History, error)
}

// ActressAliasRepositoryInterface defines the contract for actress alias operations
type ActressAliasRepositoryInterface interface {
	Create(alias *models.ActressAlias) error
	Upsert(alias *models.ActressAlias) error
	FindByAliasName(aliasName string) (*models.ActressAlias, error)
	FindByCanonicalName(canonicalName string) ([]models.ActressAlias, error)
	List() ([]models.ActressAlias, error)
	Delete(aliasName string) error
	GetAliasMap() (map[string]string, error)
}

// WordReplacementRepositoryInterface defines the contract for word replacement operations
type WordReplacementRepositoryInterface interface {
	Create(replacement *models.WordReplacement) error
	Upsert(replacement *models.WordReplacement) error
	FindByOriginal(original string) (*models.WordReplacement, error)
	List() ([]models.WordReplacement, error)
	Delete(original string) error
	GetReplacementMap() (map[string]string, error)
}

// MovieTagRepositoryInterface defines the contract for movie tag operations
type MovieTagRepositoryInterface interface {
	AddTag(movieID, tag string) error
	RemoveTag(movieID, tag string) error
	RemoveAllTags(movieID string) error
	GetTagsForMovie(movieID string) ([]string, error)
	GetMoviesWithTag(tag string) ([]string, error)
	ListAll() (map[string][]string, error)
	GetUniqueTagsList() ([]string, error)
}

// ContentIDMappingRepositoryInterface defines the contract for content ID mapping operations
type ContentIDMappingRepositoryInterface interface {
	FindBySearchID(searchID string) (*models.ContentIDMapping, error)
	Create(mapping *models.ContentIDMapping) error
	Delete(searchID string) error
	GetAll() ([]models.ContentIDMapping, error)
}

// JobRepositoryInterface defines the contract for job database operations
type JobRepositoryInterface interface {
	Create(job *models.Job) error
	Update(job *models.Job) error
	Upsert(job *models.Job) error
	FindByID(id string) (*models.Job, error)
	List() ([]models.Job, error)
	Delete(id string) error
	DeleteOrganizedOlderThan(date time.Time) error
}

// BatchFileOperationRepositoryInterface defines the contract for batch file operation operations
type BatchFileOperationRepositoryInterface interface {
	Create(op *models.BatchFileOperation) error
	CreateBatch(ops []*models.BatchFileOperation) error
	FindByID(id uint) (*models.BatchFileOperation, error)
	FindByBatchJobID(batchJobID string) ([]models.BatchFileOperation, error)
	FindByBatchJobIDAndRevertStatus(batchJobID string, revertStatus string) ([]models.BatchFileOperation, error)
	Update(op *models.BatchFileOperation) error
	UpdateRevertStatus(id uint, status string) error
	CountByBatchJobID(batchJobID string) (int64, error)
	CountByBatchJobIDAndRevertStatus(batchJobID string, status string) (int64, error)
}

// EventFilter holds optional filter parameters for composable event queries
type EventFilter struct {
	EventType string
	Severity  string
	Source    string
	Start     *time.Time
	End       *time.Time
}

// EventRepositoryInterface defines the contract for structured event logging operations
type EventRepositoryInterface interface {
	Create(event *models.Event) error
	FindByID(id uint) (*models.Event, error)
	FindByType(eventType string, limit, offset int) ([]models.Event, error)
	FindBySeverity(severity string, limit, offset int) ([]models.Event, error)
	FindByTypeAndSeverity(eventType, severity string, limit, offset int) ([]models.Event, error)
	FindBySource(source string, limit, offset int) ([]models.Event, error)
	FindByDateRange(start, end time.Time, limit, offset int) ([]models.Event, error)
	FindFiltered(filter EventFilter, limit, offset int) ([]models.Event, error)
	CountFiltered(filter EventFilter) (int64, error)
	List(limit, offset int) ([]models.Event, error)
	Count() (int64, error)
	CountByType(eventType string) (int64, error)
	CountBySeverity(severity string) (int64, error)
	CountByTypeAndSeverity(eventType, severity string) (int64, error)
	CountBySource(source string) (int64, error)
	CountGroupBySource() (map[string]int64, error)
	CountByDateRange(start, end time.Time) (int64, error)
	DeleteOlderThan(date time.Time) error
}
