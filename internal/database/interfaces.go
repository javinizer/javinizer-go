package database

import (
	"context"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// MovieRepositoryInterface defines the contract for movie database operations
type MovieRepositoryInterface interface {
	Create(ctx context.Context, movie *models.Movie) error
	Update(ctx context.Context, movie *models.Movie) error
	Upsert(ctx context.Context, movie *models.Movie) (*models.Movie, error)
	UpsertWithTranslations(ctx context.Context, movie *models.Movie, genreTranslations []models.GenreTranslationData, actressTranslations []models.ActressTranslationData) (*models.Movie, error)
	FindByID(ctx context.Context, id string) (*models.Movie, error)
	FindByContentID(ctx context.Context, contentID string) (*models.Movie, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, limit, offset int) ([]models.Movie, error)
}

// ActressRepositoryInterface defines the contract for actress database operations
type ActressRepositoryInterface interface {
	Create(ctx context.Context, actress *models.Actress) error
	Update(ctx context.Context, actress *models.Actress) error
	RenameNameFields(ctx context.Context, id uint, firstName, lastName, japaneseName string) error
	FindByID(ctx context.Context, id uint) (*models.Actress, error)
	FindByDMMID(ctx context.Context, dmmID int) (*models.Actress, error)
	FindByFirstNameLastName(ctx context.Context, firstName, lastName string) (*models.Actress, error)
	FindByJapaneseName(ctx context.Context, name string) (*models.Actress, error)
	FindByJapaneseNameAndDMMID(ctx context.Context, name string, dmmID int) (*models.Actress, error)
	FindOrCreate(ctx context.Context, actress *models.Actress) error
	List(ctx context.Context, limit, offset int) ([]models.Actress, error)
	ListAll(ctx context.Context) ([]models.Actress, error)
	ListSorted(ctx context.Context, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error)
	Search(ctx context.Context, query string) ([]models.Actress, error)
	SearchPagedSorted(ctx context.Context, query string, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error)
	Count(ctx context.Context) (int64, error)
	CountSearch(ctx context.Context, query string) (int64, error)
	Delete(ctx context.Context, id uint) error
	PreviewMerge(ctx context.Context, targetID, sourceID uint) (*ActressMergePreview, error)
	Merge(ctx context.Context, targetID, sourceID uint, resolutions map[string]string) (*ActressMergeResult, error)
}

// GenreTranslationRepositoryInterface defines the contract for genre translation operations
type GenreTranslationRepositoryInterface interface {
	Upsert(ctx context.Context, translation *models.GenreTranslation) error
	FindByGenreAndLanguage(ctx context.Context, genreID uint, language string) (*models.GenreTranslation, error)
	FindAllByGenre(ctx context.Context, genreID uint) ([]models.GenreTranslation, error)
	FindByGenreIDsAndLanguage(ctx context.Context, genreIDs []uint, language string) (map[uint][]models.GenreTranslation, error)
	Delete(ctx context.Context, genreID uint, language string) error
}

// ActressTranslationRepositoryInterface defines the contract for actress translation operations
type ActressTranslationRepositoryInterface interface {
	Upsert(ctx context.Context, translation *models.ActressTranslation) error
	FindByActressAndLanguage(ctx context.Context, actressID uint, language string) (*models.ActressTranslation, error)
	FindAllByActress(ctx context.Context, actressID uint) ([]models.ActressTranslation, error)
	FindByActressIDsAndLanguage(ctx context.Context, actressIDs []uint, language string) (map[uint][]models.ActressTranslation, error)
	Delete(ctx context.Context, actressID uint, language string) error
}

// GenreReplacementRepositoryInterface defines the contract for genre replacement operations
type GenreReplacementRepositoryInterface interface {
	Create(ctx context.Context, replacement *models.GenreReplacement) error
	Upsert(ctx context.Context, replacement *models.GenreReplacement) error
	FindByOriginal(ctx context.Context, original string) (*models.GenreReplacement, error)
	FindByID(ctx context.Context, id uint) (*models.GenreReplacement, error)
	List(ctx context.Context) ([]models.GenreReplacement, error)
	Delete(ctx context.Context, original string) error
	DeleteByID(ctx context.Context, id uint) error
	GetReplacementMap(ctx context.Context) (map[string]string, error)
}

// HistoryRepositoryInterface defines the contract for history tracking operations
type HistoryRepositoryInterface interface {
	Create(ctx context.Context, history *models.History) error
	FindByID(ctx context.Context, id uint) (*models.History, error)
	FindByMovieID(ctx context.Context, movieID string) ([]models.History, error)
	// ListByMovieID returns a paginated slice of history records for the given movie ID.
	ListByMovieID(ctx context.Context, movieID string, limit, offset int) ([]models.History, error)
	// CountByMovieID returns the total number of history records for the given movie ID.
	CountByMovieID(ctx context.Context, movieID string) (int64, error)
	FindByBatchJobID(ctx context.Context, batchJobID string) ([]models.History, error)
	FindByOperation(ctx context.Context, operation models.HistoryOperation, limit int) ([]models.History, error)
	// ListByOperation returns a paginated slice of history records for the given operation.
	ListByOperation(ctx context.Context, operation models.HistoryOperation, limit, offset int) ([]models.History, error)
	FindByStatus(ctx context.Context, status models.HistoryStatus, limit int) ([]models.History, error)
	// ListByStatus returns a paginated slice of history records for the given status.
	ListByStatus(ctx context.Context, status models.HistoryStatus, limit, offset int) ([]models.History, error)
	FindRecent(ctx context.Context, limit int) ([]models.History, error)
	FindByDateRange(ctx context.Context, start, end time.Time) ([]models.History, error)
	Count(ctx context.Context) (int64, error)
	CountByStatus(ctx context.Context, status models.HistoryStatus) (int64, error)
	CountByOperation(ctx context.Context, operation models.HistoryOperation) (int64, error)
	Delete(ctx context.Context, id uint) error
	DeleteByMovieID(ctx context.Context, movieID string) error
	DeleteOlderThan(ctx context.Context, date time.Time) error
	List(ctx context.Context, limit, offset int) ([]models.History, error)
}

// ActressAliasRepositoryInterface defines the contract for actress alias operations
type ActressAliasRepositoryInterface interface {
	Create(ctx context.Context, alias *models.ActressAlias) error
	Upsert(ctx context.Context, alias *models.ActressAlias) error
	FindByAliasName(ctx context.Context, aliasName string) (*models.ActressAlias, error)
	FindByCanonicalName(ctx context.Context, canonicalName string) ([]models.ActressAlias, error)
	List(ctx context.Context) ([]models.ActressAlias, error)
	Delete(ctx context.Context, aliasName string) error
	GetAliasMap(ctx context.Context) (map[string]string, error)
}

// MovieTagRepositoryInterface defines the contract for movie tag operations
type MovieTagRepositoryInterface interface {
	AddTag(ctx context.Context, movieID, tag string) error
	RemoveTag(ctx context.Context, movieID, tag string) error
	RemoveAllTags(ctx context.Context, movieID string) error
	GetTagsForMovie(ctx context.Context, movieID string) ([]string, error)
	GetMoviesWithTag(ctx context.Context, tag string) ([]string, error)
	ListTagsPaginated(ctx context.Context, limit, offset int) ([]models.MovieTag, error)
	ListAll(ctx context.Context) (map[string][]string, error)
	ListAllChunked(ctx context.Context, chunkSize int) (map[string][]string, error)
	GetUniqueTagsList(ctx context.Context) ([]string, error)
}

// ContentIDMappingRepositoryInterface is an alias for models.ContentIDMappingRepositoryInterface.
// The canonical definition lives in models to avoid import cycles (scraperutil → database → config).
type ContentIDMappingRepositoryInterface = models.ContentIDMappingRepositoryInterface

// JobRepositoryInterface defines the contract for job database operations
type JobRepositoryInterface interface {
	Create(ctx context.Context, job *models.Job) error
	Update(ctx context.Context, job *models.Job) error
	Upsert(ctx context.Context, job *models.Job) error
	FindByID(ctx context.Context, id string) (*models.Job, error)
	List(ctx context.Context) ([]models.Job, error)
	Delete(ctx context.Context, id string) error
	DeleteOrganizedOlderThan(ctx context.Context, date time.Time) error
}

// BatchFileOperationRepositoryInterface defines the contract for batch file operation operations
type BatchFileOperationRepositoryInterface interface {
	Create(ctx context.Context, op *models.BatchFileOperation) error
	CreateBatch(ctx context.Context, ops []*models.BatchFileOperation) error
	FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error)
	FindByBatchJobID(ctx context.Context, batchJobID string) ([]models.BatchFileOperation, error)
	FindByBatchJobIDAndRevertStatus(ctx context.Context, batchJobID string, revertStatus models.RevertStatusEnum) ([]models.BatchFileOperation, error)
	Update(ctx context.Context, op *models.BatchFileOperation) error
	UpdateRevertStatus(ctx context.Context, id uint, status models.RevertStatusEnum) error
	CountByBatchJobID(ctx context.Context, batchJobID string) (int64, error)
	CountByBatchJobIDAndRevertStatus(ctx context.Context, batchJobID string, status models.RevertStatusEnum) (int64, error)
	// CountByBatchJobIDs returns a map of jobID→count for all given job IDs in a single query.
	// Used by the batch job listing endpoint to avoid N+1 queries.
	CountByBatchJobIDs(ctx context.Context, jobIDs []string) (map[string]int64, error)
	// CountRevertedByBatchJobIDs returns a map of jobID→reverted count for all given job IDs.
	CountRevertedByBatchJobIDs(ctx context.Context, jobIDs []string) (map[string]int64, error)
}

// ApiTokenRepositoryInterface defines the contract for API token operations
type ApiTokenRepositoryInterface interface {
	Create(ctx context.Context, token *models.ApiToken) error
	FindByID(ctx context.Context, id string) (*models.ApiToken, error)
	FindByTokenHash(ctx context.Context, hash string) (*models.ApiToken, error)
	FindByPrefix(ctx context.Context, prefix string) (*models.ApiToken, error)
	ListActive(ctx context.Context) ([]models.ApiToken, error)
	Revoke(ctx context.Context, id string) error
	UpdateLastUsed(ctx context.Context, id string) error
	Regenerate(ctx context.Context, id string, newHash string, newPrefix string) (*models.ApiToken, error)
}

// EventFilter holds optional filter parameters for composable event queries
type EventFilter struct {
	EventType models.EventCategory
	Severity  models.EventSeverity
	Source    string
	Start     *time.Time
	End       *time.Time
}

// EventRepositoryInterface defines the contract for structured event logging operations
type EventRepositoryInterface interface {
	Create(ctx context.Context, event *models.Event) error
	FindByID(ctx context.Context, id uint) (*models.Event, error)
	FindFiltered(ctx context.Context, filter EventFilter, limit, offset int) ([]models.Event, error)
	CountFiltered(ctx context.Context, filter EventFilter) (int64, error)
	List(ctx context.Context, limit, offset int) ([]models.Event, error)
	Count(ctx context.Context) (int64, error)
	CountGroupBySource(ctx context.Context) (map[string]int64, error)
	DeleteOlderThan(ctx context.Context, date time.Time) (int64, error)
}

// WordReplacementRepositoryInterface defines the contract for word replacement operations
type WordReplacementRepositoryInterface interface {
	Create(ctx context.Context, replacement *models.WordReplacement) error
	Upsert(ctx context.Context, replacement *models.WordReplacement) error
	FindByOriginal(ctx context.Context, original string) (*models.WordReplacement, error)
	FindByID(ctx context.Context, id uint) (*models.WordReplacement, error)
	List(ctx context.Context) ([]models.WordReplacement, error)
	Delete(ctx context.Context, original string) error
	DeleteByID(ctx context.Context, id uint) error
	GetReplacementMap(ctx context.Context) (map[string]string, error)
}

// GenreRepositoryInterface defines the contract for genre database operations
type GenreRepositoryInterface interface {
	FindOrCreate(ctx context.Context, name string) (*models.Genre, error)
	List(ctx context.Context) ([]models.Genre, error)
}
