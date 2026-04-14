package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB wraps the GORM database connection
type DB struct {
	*gorm.DB
	dsn string
}

var sqliteMemoryDSNCounter atomic.Uint64

// parseLogLevel converts a log level string to a GORM logger.LogLevel
// Normalizes input by trimming whitespace and converting to lowercase
// Returns logger.Silent for invalid values with a warning
func parseLogLevel(level string) logger.LogLevel {
	// Normalize input: trim whitespace and convert to lowercase for case-insensitive comparison
	normalized := strings.ToLower(strings.TrimSpace(level))

	switch normalized {
	case "info":
		return logger.Info
	case "warn":
		return logger.Warn
	case "error":
		return logger.Error
	case "silent", "":
		return logger.Silent
	default:
		// Invalid log level provided - warn and default to silent
		log.Printf("Warning: invalid database log_level '%s', defaulting to 'silent'. Valid options: silent, error, warn, info\n", level)
		return logger.Silent
	}
}

// New creates a new database connection
func New(cfg *config.Config) (*DB, error) {
	var dialector gorm.Dialector

	switch cfg.Database.Type {
	case "sqlite", "":
		dialector = sqlite.Open(normalizeSQLiteDSN(cfg.Database.DSN))
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Database.Type)
	}

	// Configure database logger level (independent from app logging)
	logLevel := parseLogLevel(cfg.Database.LogLevel)

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return &DB{
		DB:  db,
		dsn: cfg.Database.DSN,
	}, nil
}

// AutoMigrate runs startup database migrations.
//
// Kept for backward compatibility in tests and existing call sites.
// New runtime paths should call RunMigrationsOnStartup directly.
func (db *DB) AutoMigrate() error {
	return db.RunMigrationsOnStartup(context.Background())
}

// SqliteTimeFormat is used to format time.Time values for SQLite datetime comparisons.
// SQLite stores timestamps as TEXT in inconsistent formats (RFC3339 with T/Z, with
// fractional seconds, etc.) and GORM binds time.Time as "2006-01-02 15:04:05" (space,
// no TZ). Direct TEXT comparison between these formats produces wrong results because
// 'T' > ' ' and fractional seconds alter lexicographic order. Wrapping both sides in
// datetime() normalizes to a consistent format before comparison.
const SqliteTimeFormat = "2006-01-02 15:04:05"

func normalizeSQLiteDSN(dsn string) string {
	normalized := strings.ToLower(strings.TrimSpace(dsn))
	if normalized != ":memory:" {
		return dsn
	}
	// `:memory:` is scoped per SQLite connection. Goose migration checks and applies
	// can use multiple connections, so convert to a unique shared-cache memory URI.
	next := sqliteMemoryDSNCounter.Add(1)
	return fmt.Sprintf("file:javinizer_mem_%d_%d?mode=memory&cache=shared", time.Now().UnixNano(), next)
}

// Close closes the database connection
func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// MovieRepository provides database operations for movies
type MovieRepository struct {
	db *DB
}

// NewMovieRepository creates a new movie repository
func NewMovieRepository(db *DB) *MovieRepository {
	return &MovieRepository{db: db}
}

// Create saves a new movie to the database
func (r *MovieRepository) Create(movie *models.Movie) error {
	return r.db.Create(movie).Error
}

// Update updates an existing movie
func (r *MovieRepository) Update(movie *models.Movie) error {
	return r.db.Save(movie).Error
}

// Upsert creates a new movie or updates if it already exists (by ID)
func (r *MovieRepository) Upsert(movie *models.Movie) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Validate and normalize ContentID (primary key)
		if strings.TrimSpace(movie.ContentID) == "" {
			if strings.TrimSpace(movie.ID) == "" {
				return fmt.Errorf("content_id is required when using ContentID as primary key")
			}
			// Pragmatic fallback: derive from display ID
			movie.ContentID = strings.ToLower(strings.ReplaceAll(movie.ID, "-", ""))
		}

		// Drop actress entries with no identifying information before any association work.
		// This prevents zero-value placeholders from being persisted as real actress records.
		movie.Actresses = filterIdentifiableActresses(movie.Actresses)

		// Check if movie exists - prefer ContentID (primary key) with fallback to display ID
		var existing models.Movie
		var existingFound bool
		if movie.ContentID != "" {
			err := tx.Select("content_id", "created_at").First(&existing, "content_id = ?", movie.ContentID).Error
			if err == nil {
				existingFound = true
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if !existingFound && movie.ID != "" {
			// Fallback to display ID for backward compatibility
			err := tx.Select("content_id", "created_at").First(&existing, "id = ?", movie.ID).Error
			if err == nil {
				existingFound = true
				// CRITICAL: Synchronize ContentID to ensure we update the existing record
				movie.ContentID = existing.ContentID
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		if !existingFound {
			// Movie doesn't exist, create it with associations
			// First, ensure genres and actresses exist or get their IDs
			if err := r.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return err
			}
			if err := r.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}

			// Save translations separately to avoid UNIQUE constraint violations
			translations := movie.Translations
			movie.Translations = nil

			// Create the movie record (without translations)
			// Omit Actresses and Genres to prevent GORM from auto-processing associations with ID=0
			if err := tx.Omit("Actresses", "Genres").Create(movie).Error; err != nil {
				// Handle race condition: another transaction may have inserted the same ContentID
				if errors.Is(err, gorm.ErrDuplicatedKey) {
					// Reload the existing record and switch to update path
					var existingMovie models.Movie
					if loadErr := tx.Select("created_at").First(&existingMovie, "content_id = ?", movie.ContentID).Error; loadErr == nil {
						// Record exists, preserve CreatedAt and proceed with update
						movie.CreatedAt = existingMovie.CreatedAt

						// Update the movie record (without translations)
						// Omit Actresses and Genres to prevent GORM from auto-processing associations with ID=0
						if err := tx.Omit("Actresses", "Genres").Save(movie).Error; err != nil {
							return err
						}

						// Replace associations
						if err := tx.Model(movie).Association("Genres").Replace(movie.Genres); err != nil {
							return err
						}
						if err := tx.Model(movie).Association("Actresses").Replace(movie.Actresses); err != nil {
							return err
						}

						// Upsert translations individually
						translationRepo := NewMovieTranslationRepository(r.db)
						for i := range translations {
							translations[i].MovieID = movie.ContentID
							if err := translationRepo.UpsertTx(tx, &translations[i]); err != nil {
								return err
							}
						}

						// Restore translations to movie object
						movie.Translations = translations
						return nil
					}
				}
				return err
			}

			// Create associations
			if len(movie.Genres) > 0 {
				if err := tx.Model(movie).Association("Genres").Replace(movie.Genres); err != nil {
					return err
				}
			}
			if len(movie.Actresses) > 0 {
				if err := tx.Model(movie).Association("Actresses").Replace(movie.Actresses); err != nil {
					return err
				}
			}

			// Upsert translations individually to avoid UNIQUE constraint violations
			translationRepo := NewMovieTranslationRepository(r.db)
			for i := range translations {
				translations[i].MovieID = movie.ContentID
				if err := translationRepo.UpsertTx(tx, &translations[i]); err != nil {
					return err
				}
			}

			// Restore translations to movie object
			movie.Translations = translations

			return nil
		}

		// Movie exists, update it
		// Preserve the CreatedAt timestamp from the existing record
		movie.CreatedAt = existing.CreatedAt

		// Set the MovieID for all translations
		for i := range movie.Translations {
			movie.Translations[i].MovieID = movie.ContentID
		}

		// Ensure genres and actresses exist
		if err := r.ensureGenresExistTx(tx, movie.Genres); err != nil {
			return err
		}
		if err := r.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}

		// Save translations separately - temporarily remove them to avoid Save() trying to insert them
		translations := movie.Translations
		movie.Translations = nil

		// Update the movie record (without translations)
		// Omit Actresses and Genres to prevent GORM from auto-processing associations with ID=0
		if err := tx.Omit("Actresses", "Genres").Save(movie).Error; err != nil {
			return err
		}

		// Replace associations
		if err := tx.Model(movie).Association("Genres").Replace(movie.Genres); err != nil {
			return err
		}
		if err := tx.Model(movie).Association("Actresses").Replace(movie.Actresses); err != nil {
			return err
		}

		// Upsert translations individually to avoid UNIQUE constraint violations
		translationRepo := NewMovieTranslationRepository(r.db)
		for i := range translations {
			translations[i].MovieID = movie.ContentID
			if err := translationRepo.UpsertTx(tx, &translations[i]); err != nil {
				return err
			}
		}

		// Restore translations to movie object
		movie.Translations = translations

		return nil
	})
}

// ensureGenresExistTx ensures all genres exist in DB using the provided transaction
func (r *MovieRepository) ensureGenresExistTx(tx *gorm.DB, genres []models.Genre) error {
	for i := range genres {
		var existing models.Genre
		err := tx.Where("name = ?", genres[i].Name).First(&existing).Error
		if err == nil {
			// Genre exists, use its ID
			genres[i] = existing
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			// Genre doesn't exist, create it
			if err := tx.Create(&genres[i]).Error; err != nil {
				// Might be a race condition, try to find it again
				if err := tx.Where("name = ?", genres[i].Name).First(&existing).Error; err == nil {
					genres[i] = existing
				} else {
					return err
				}
			}
		} else {
			// Database error (connection, timeout, etc.) - fail fast
			return err
		}
	}
	return nil
}

// ensureActressesExist ensures all actresses exist in DB, gets or creates them
// mergeActressData updates existing actress with new data if available
func (r *MovieRepository) mergeActressData(existing *models.Actress, new models.Actress) bool {
	needsUpdate := false

	// Update ThumbURL if we have a new one and existing doesn't have one
	if new.ThumbURL != "" && existing.ThumbURL == "" {
		existing.ThumbURL = new.ThumbURL
		needsUpdate = true
	}

	// Update FirstName/LastName if we have new ones and existing doesn't
	if new.FirstName != "" && existing.FirstName == "" {
		existing.FirstName = new.FirstName
		needsUpdate = true
	}
	if new.LastName != "" && existing.LastName == "" {
		existing.LastName = new.LastName
		needsUpdate = true
	}

	return needsUpdate
}

func (r *MovieRepository) ensureActressesExistTx(tx *gorm.DB, actresses []models.Actress) error {
	for i := range actresses {
		var existing models.Actress
		var err error

		// Try to find actress using DMMID if available (primary unique identifier)
		if actresses[i].DMMID != 0 {
			err = tx.Where("dmm_id = ?", actresses[i].DMMID).First(&existing).Error
		} else if actresses[i].JapaneseName != "" {
			// Fall back to japanese_name if DMMID is not available
			err = tx.Where("japanese_name = ?", actresses[i].JapaneseName).First(&existing).Error
		} else if actresses[i].FirstName != "" || actresses[i].LastName != "" {
			// Fall back to first_name and last_name if neither DMMID nor japanese_name is available
			// Build query based on which fields are present
			if actresses[i].FirstName != "" && actresses[i].LastName != "" {
				err = tx.Where("first_name = ? AND last_name = ?", actresses[i].FirstName, actresses[i].LastName).First(&existing).Error
			} else if actresses[i].FirstName != "" {
				err = tx.Where("first_name = ?", actresses[i].FirstName).First(&existing).Error
			} else {
				err = tx.Where("last_name = ?", actresses[i].LastName).First(&existing).Error
			}
		} else {
			// Skip actresses with no identifying information
			continue
		}

		if err == nil {
			// Actress exists, merge new data and update if needed
			if r.mergeActressData(&existing, actresses[i]) {
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
			}
			actresses[i] = existing
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			// Actress doesn't exist, create them
			if err := tx.Create(&actresses[i]).Error; err != nil {
				// Might be a race condition, try to find again
				if actresses[i].DMMID != 0 {
					if err := tx.Where("dmm_id = ?", actresses[i].DMMID).First(&existing).Error; err == nil {
						if r.mergeActressData(&existing, actresses[i]) {
							if err := tx.Save(&existing).Error; err != nil {
								return err
							}
						}
						actresses[i] = existing
						continue
					}
				} else if actresses[i].JapaneseName != "" {
					if err := tx.Where("japanese_name = ?", actresses[i].JapaneseName).First(&existing).Error; err == nil {
						if r.mergeActressData(&existing, actresses[i]) {
							if err := tx.Save(&existing).Error; err != nil {
								return err
							}
						}
						actresses[i] = existing
						continue
					}
				} else if actresses[i].FirstName != "" || actresses[i].LastName != "" {
					var retryErr error
					// Build query based on which fields are present
					if actresses[i].FirstName != "" && actresses[i].LastName != "" {
						retryErr = tx.Where("first_name = ? AND last_name = ?", actresses[i].FirstName, actresses[i].LastName).First(&existing).Error
					} else if actresses[i].FirstName != "" {
						retryErr = tx.Where("first_name = ?", actresses[i].FirstName).First(&existing).Error
					} else {
						retryErr = tx.Where("last_name = ?", actresses[i].LastName).First(&existing).Error
					}
					if retryErr == nil {
						if r.mergeActressData(&existing, actresses[i]) {
							if err := tx.Save(&existing).Error; err != nil {
								return err
							}
						}
						actresses[i] = existing
						continue
					}
				}
				return err
			}
		} else {
			// Database error (connection, timeout, etc.) - fail fast
			return err
		}
	}
	return nil
}

// FindByID finds a movie by its ID
func (r *MovieRepository) FindByID(id string) (*models.Movie, error) {
	var movie models.Movie
	err := r.db.Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &movie, nil
}

// FindByContentID finds a movie by its content ID
func (r *MovieRepository) FindByContentID(contentID string) (*models.Movie, error) {
	var movie models.Movie
	err := r.db.Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "content_id = ?", contentID).Error
	if err != nil {
		return nil, err
	}
	return &movie, nil
}

// Delete deletes a movie by ID
func (r *MovieRepository) Delete(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Lightweight query: fetch only ContentID (avoid expensive Preloads)
		var movie models.Movie
		if err := tx.Model(&models.Movie{}).
			Select("content_id").
			Where("id = ?", id).
			First(&movie).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // Already deleted
			}
			return err
		}

		// If contentID is empty, the movie doesn't exist
		if movie.ContentID == "" {
			return nil // Already deleted
		}

		// Clear many-to-many associations (CRITICAL for data integrity)
		stub := &models.Movie{ContentID: movie.ContentID}
		if err := tx.Model(stub).Association("Actresses").Clear(); err != nil {
			return err
		}
		if err := tx.Model(stub).Association("Genres").Clear(); err != nil {
			return err
		}

		// Delete translations (MovieID references ContentID)
		if err := tx.Delete(&models.MovieTranslation{}, "movie_id = ?", movie.ContentID).Error; err != nil {
			return err
		}

		// Delete tags (MovieID references ContentID)
		if err := tx.Delete(&models.MovieTag{}, "movie_id = ?", movie.ContentID).Error; err != nil {
			return err
		}

		// Finally, delete the movie by primary key
		return tx.Delete(&models.Movie{}, "content_id = ?", movie.ContentID).Error
	})
}

// List returns a paginated list of movies
func (r *MovieRepository) List(limit, offset int) ([]models.Movie, error) {
	var movies []models.Movie
	err := r.db.Preload("Actresses").Preload("Genres").Limit(limit).Offset(offset).Find(&movies).Error
	return movies, err
}

// ActressRepository provides database operations for actresses
type ActressRepository struct {
	db *DB
}

var (
	ErrActressMergeSameID           = errors.New("target_id and source_id must be different")
	ErrActressMergeInvalidID        = errors.New("target_id and source_id must be greater than 0")
	ErrActressMergeInvalidField     = errors.New("invalid merge field")
	ErrActressMergeInvalidDecision  = errors.New("invalid merge resolution")
	ErrActressMergeUniqueConstraint = errors.New("merge would violate unique constraints")
)

type ActressMergeConflict struct {
	Field             string      `json:"field"`
	TargetValue       interface{} `json:"target_value,omitempty"`
	SourceValue       interface{} `json:"source_value,omitempty"`
	DefaultResolution string      `json:"default_resolution"`
}

type ActressMergePreview struct {
	Target             models.Actress                  `json:"target"`
	Source             models.Actress                  `json:"source"`
	ProposedMerged     models.Actress                  `json:"proposed_merged"`
	Conflicts          []ActressMergeConflict          `json:"conflicts"`
	DefaultResolutions map[string]string               `json:"default_resolutions"`
	ConflictByField    map[string]ActressMergeConflict `json:"-"`
}

type ActressMergeResult struct {
	MergedActress     models.Actress `json:"merged_actress"`
	MergedFromID      uint           `json:"merged_from_id"`
	UpdatedMovies     int            `json:"updated_movies"`
	ConflictsResolved int            `json:"conflicts_resolved"`
	AliasesAdded      int            `json:"aliases_added"`
}

// NewActressRepository creates a new actress repository
func NewActressRepository(db *DB) *ActressRepository {
	return &ActressRepository{db: db}
}

// Create saves a new actress to the database
func (r *ActressRepository) Create(actress *models.Actress) error {
	return r.db.Create(actress).Error
}

// Update updates an existing actress
func (r *ActressRepository) Update(actress *models.Actress) error {
	return r.db.Save(actress).Error
}

// FindByID finds an actress by numeric ID.
func (r *ActressRepository) FindByID(id uint) (*models.Actress, error) {
	var actress models.Actress
	err := r.db.First(&actress, id).Error
	if err != nil {
		return nil, err
	}
	return &actress, nil
}

// Delete removes an actress by numeric ID.
func (r *ActressRepository) Delete(id uint) error {
	return r.db.Delete(&models.Actress{}, id).Error
}

// Count returns total actresses in database.
func (r *ActressRepository) Count() (int64, error) {
	var count int64
	err := r.db.Model(&models.Actress{}).Count(&count).Error
	return count, err
}

// FindByJapaneseName finds an actress by Japanese name
func (r *ActressRepository) FindByJapaneseName(name string) (*models.Actress, error) {
	var actress models.Actress
	err := r.db.First(&actress, "japanese_name = ?", name).Error
	if err != nil {
		return nil, err
	}
	return &actress, nil
}

// FindOrCreate finds an actress or creates a new one
func (r *ActressRepository) FindOrCreate(actress *models.Actress) error {
	// Try to find by Japanese name first
	if actress.JapaneseName != "" {
		existing, err := r.FindByJapaneseName(actress.JapaneseName)
		if err == nil {
			*actress = *existing
			return nil
		}
	}

	// If not found, create new
	return r.Create(actress)
}

// List returns a paginated list of actresses
func (r *ActressRepository) List(limit, offset int) ([]models.Actress, error) {
	var actresses []models.Actress
	err := r.db.Order("japanese_name ASC, last_name ASC, first_name ASC, id ASC").Limit(limit).Offset(offset).Find(&actresses).Error
	return actresses, err
}

// ListSorted returns a paginated list of actresses with explicit sorting.
func (r *ActressRepository) ListSorted(limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder = normalizeActressSort(sortBy, sortOrder)
	dbq := r.db.DB
	for _, clause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(clause)
	}

	err := dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	return actresses, err
}

// SearchPaged searches actresses by name and supports pagination.
func (r *ActressRepository) SearchPaged(query string, limit, offset int) ([]models.Actress, error) {
	var actresses []models.Actress

	searchPattern := "%" + query + "%"
	err := r.db.Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern).
		Order("japanese_name ASC, last_name ASC, first_name ASC, id ASC").
		Limit(limit).
		Offset(offset).
		Find(&actresses).Error
	return actresses, err
}

// SearchPagedSorted searches actresses by name with pagination and explicit sorting.
func (r *ActressRepository) SearchPagedSorted(query string, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder = normalizeActressSort(sortBy, sortOrder)
	searchPattern := "%" + query + "%"

	dbq := r.db.Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern)
	for _, clause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(clause)
	}

	err := dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	return actresses, err
}

// CountSearch returns count of actresses matching a query.
func (r *ActressRepository) CountSearch(query string) (int64, error) {
	var count int64
	searchPattern := "%" + query + "%"
	err := r.db.Model(&models.Actress{}).
		Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
			searchPattern, searchPattern, searchPattern).
		Count(&count).Error
	return count, err
}

// Search searches for actresses by name (first, last, or Japanese name)
// If query is empty, returns all actresses (limited to 100)
func (r *ActressRepository) Search(query string) ([]models.Actress, error) {
	var actresses []models.Actress

	// If query is empty, return all actresses
	if query == "" {
		err := r.db.Limit(100).Order("japanese_name ASC, last_name ASC, first_name ASC").Find(&actresses).Error
		return actresses, err
	}

	// Otherwise search by pattern
	searchPattern := "%" + query + "%"
	err := r.db.Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern).
		Order("japanese_name ASC, last_name ASC, first_name ASC").
		Limit(20). // Limit results to prevent too many matches
		Find(&actresses).Error
	return actresses, err
}

func (r *ActressRepository) loadPair(targetID, sourceID uint) (*models.Actress, *models.Actress, error) {
	if targetID == 0 || sourceID == 0 {
		return nil, nil, ErrActressMergeInvalidID
	}
	if targetID == sourceID {
		return nil, nil, ErrActressMergeSameID
	}

	target, err := r.FindByID(targetID)
	if err != nil {
		return nil, nil, err
	}
	source, err := r.FindByID(sourceID)
	if err != nil {
		return nil, nil, err
	}
	return target, source, nil
}

func (r *ActressRepository) PreviewMerge(targetID, sourceID uint) (*ActressMergePreview, error) {
	target, source, err := r.loadPair(targetID, sourceID)
	if err != nil {
		return nil, err
	}

	conflicts := buildActressMergeConflicts(target, source)
	defaultResolutions := defaultResolutionsFromConflicts(conflicts)
	merged, err := mergeActressValues(target, source, defaultResolutions)
	if err != nil {
		return nil, err
	}

	canonicalName := canonicalActressName(&merged)
	merged.Aliases, _, _ = mergeAliasValues(target.Aliases, collectActressAliasCandidates(source), canonicalName)

	byField := make(map[string]ActressMergeConflict, len(conflicts))
	for _, conflict := range conflicts {
		byField[conflict.Field] = conflict
	}

	return &ActressMergePreview{
		Target:             *target,
		Source:             *source,
		ProposedMerged:     merged,
		Conflicts:          conflicts,
		DefaultResolutions: defaultResolutions,
		ConflictByField:    byField,
	}, nil
}

func (r *ActressRepository) Merge(targetID, sourceID uint, resolutions map[string]string) (*ActressMergeResult, error) {
	preview, err := r.PreviewMerge(targetID, sourceID)
	if err != nil {
		return nil, err
	}

	normalizedResolutions, err := normalizeMergeResolutions(resolutions)
	if err != nil {
		return nil, err
	}
	for _, conflict := range preview.Conflicts {
		if _, exists := normalizedResolutions[conflict.Field]; !exists {
			normalizedResolutions[conflict.Field] = "target"
		}
	}

	merged, err := mergeActressValues(&preview.Target, &preview.Source, normalizedResolutions)
	if err != nil {
		return nil, err
	}

	canonicalName := canonicalActressName(&merged)
	aliasesAdded := 0
	sourceCandidates := collectActressAliasCandidates(&preview.Source)
	merged.Aliases, aliasesAdded, _ = mergeAliasValues(
		preview.Target.Aliases,
		sourceCandidates,
		canonicalName,
	)
	sourceAliasUpserts := sourceAliasesForUpsert(sourceCandidates, canonicalName)

	updatedMovies := 0
	conflictsResolved := len(preview.Conflicts)
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if merged.DMMID > 0 {
			var existing models.Actress
			checkErr := tx.Where("dmm_id = ? AND id NOT IN ?", merged.DMMID, []uint{targetID, sourceID}).First(&existing).Error
			if checkErr == nil {
				return fmt.Errorf("%w: dmm_id %d is already used by actress #%d", ErrActressMergeUniqueConstraint, merged.DMMID, existing.ID)
			}
			if checkErr != nil && !errors.Is(checkErr, gorm.ErrRecordNotFound) {
				return checkErr
			}
		}

		// If target adopts source DMMID, clear source first to avoid temporary UNIQUE collisions.
		if merged.DMMID > 0 && merged.DMMID == preview.Source.DMMID && preview.Target.DMMID != preview.Source.DMMID {
			tempDMMID := -int(sourceID)
			if tempDMMID == 0 {
				tempDMMID = -1
			}
			if err := tx.Model(&models.Actress{}).Where("id = ?", sourceID).Update("dmm_id", tempDMMID).Error; err != nil {
				return err
			}
		}

		if err := tx.Model(&models.Actress{}).Where("id = ?", targetID).Updates(map[string]interface{}{
			"dmm_id":        merged.DMMID,
			"first_name":    merged.FirstName,
			"last_name":     merged.LastName,
			"japanese_name": merged.JapaneseName,
			"thumb_url":     merged.ThumbURL,
			"aliases":       merged.Aliases,
			"updated_at":    time.Now().UTC(),
		}).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrActressMergeUniqueConstraint
			}
			return err
		}

		var moveErr error
		updatedMovies, moveErr = moveMovieAssociations(tx, sourceID, targetID)
		if moveErr != nil {
			return moveErr
		}

		if err := upsertActressAliases(tx, sourceAliasUpserts, canonicalName); err != nil {
			return err
		}

		if err := tx.Delete(&models.Actress{}, sourceID).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	mergedRecord, err := r.FindByID(targetID)
	if err != nil {
		return nil, err
	}

	return &ActressMergeResult{
		MergedActress:     *mergedRecord,
		MergedFromID:      sourceID,
		UpdatedMovies:     updatedMovies,
		ConflictsResolved: conflictsResolved,
		AliasesAdded:      aliasesAdded,
	}, nil
}

// MovieTranslationRepository provides database operations for movie translations
type MovieTranslationRepository struct {
	db *DB
}

// NewMovieTranslationRepository creates a new movie translation repository
func NewMovieTranslationRepository(db *DB) *MovieTranslationRepository {
	return &MovieTranslationRepository{db: db}
}

// Upsert creates a new translation or updates if it already exists (by MovieID + Language)
func (r *MovieTranslationRepository) Upsert(translation *models.MovieTranslation) error {
	return r.UpsertTx(r.db.DB, translation)
}

// UpsertTx creates a new translation or updates if it already exists using the provided transaction
func (r *MovieTranslationRepository) UpsertTx(tx *gorm.DB, translation *models.MovieTranslation) error {
	// Try to find existing translation for this movie and language
	var existing models.MovieTranslation
	err := tx.First(&existing, "movie_id = ? AND language = ?", translation.MovieID, translation.Language).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// Translation doesn't exist, create it
		if err := tx.Create(translation).Error; err != nil {
			// Handle race condition: another transaction may have inserted the same translation
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				// Reload the existing record and switch to update path
				if loadErr := tx.First(&existing, "movie_id = ? AND language = ?", translation.MovieID, translation.Language).Error; loadErr == nil {
					translation.ID = existing.ID
					translation.CreatedAt = existing.CreatedAt
					return tx.Save(translation).Error
				}
			}
			return err
		}
		return nil
	}

	// Translation exists, update it
	translation.ID = existing.ID
	translation.CreatedAt = existing.CreatedAt
	return tx.Save(translation).Error
}

// FindByMovieAndLanguage finds a translation for a specific movie and language
func (r *MovieTranslationRepository) FindByMovieAndLanguage(movieID, language string) (*models.MovieTranslation, error) {
	var translation models.MovieTranslation
	err := r.db.First(&translation, "movie_id = ? AND language = ?", movieID, language).Error
	if err != nil {
		return nil, err
	}
	return &translation, nil
}

// FindAllByMovie finds all translations for a specific movie
func (r *MovieTranslationRepository) FindAllByMovie(movieID string) ([]models.MovieTranslation, error) {
	var translations []models.MovieTranslation
	err := r.db.Where("movie_id = ?", movieID).Find(&translations).Error
	return translations, err
}

// Delete deletes a translation
func (r *MovieTranslationRepository) Delete(movieID, language string) error {
	return r.db.Delete(&models.MovieTranslation{}, "movie_id = ? AND language = ?", movieID, language).Error
}

// GenreRepository provides database operations for genres
type GenreRepository struct {
	db *DB
}

// NewGenreRepository creates a new genre repository
func NewGenreRepository(db *DB) *GenreRepository {
	return &GenreRepository{db: db}
}

// FindOrCreate finds a genre or creates a new one
func (r *GenreRepository) FindOrCreate(name string) (*models.Genre, error) {
	var genre models.Genre
	err := r.db.FirstOrCreate(&genre, models.Genre{Name: name}).Error
	return &genre, err
}

// List returns all genres
func (r *GenreRepository) List() ([]models.Genre, error) {
	var genres []models.Genre
	err := r.db.Find(&genres).Error
	return genres, err
}

// GenreReplacementRepository provides database operations for genre replacements
type GenreReplacementRepository struct {
	db *DB
}

// NewGenreReplacementRepository creates a new genre replacement repository
func NewGenreReplacementRepository(db *DB) *GenreReplacementRepository {
	return &GenreReplacementRepository{db: db}
}

// Create adds a new genre replacement
func (r *GenreReplacementRepository) Create(replacement *models.GenreReplacement) error {
	return r.db.Create(replacement).Error
}

// Upsert creates or updates a genre replacement
func (r *GenreReplacementRepository) Upsert(replacement *models.GenreReplacement) error {
	existing, err := r.FindByOriginal(replacement.Original)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// Doesn't exist, create it
		return r.Create(replacement)
	}

	// Exists, update it
	replacement.ID = existing.ID
	replacement.CreatedAt = existing.CreatedAt
	return r.db.Save(replacement).Error
}

// FindByOriginal finds a replacement by original genre name
func (r *GenreReplacementRepository) FindByOriginal(original string) (*models.GenreReplacement, error) {
	var replacement models.GenreReplacement
	err := r.db.First(&replacement, "original = ?", original).Error
	if err != nil {
		return nil, err
	}
	return &replacement, nil
}

// List returns all genre replacements
func (r *GenreReplacementRepository) List() ([]models.GenreReplacement, error) {
	var replacements []models.GenreReplacement
	err := r.db.Find(&replacements).Error
	return replacements, err
}

// Delete removes a genre replacement
func (r *GenreReplacementRepository) Delete(original string) error {
	return r.db.Delete(&models.GenreReplacement{}, "original = ?", original).Error
}

// GetReplacementMap returns all replacements as a map[original]replacement
func (r *GenreReplacementRepository) GetReplacementMap() (map[string]string, error) {
	replacements, err := r.List()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, r := range replacements {
		result[r.Original] = r.Replacement
	}
	return result, nil
}

// HistoryRepository provides database operations for operation history
type HistoryRepository struct {
	db *DB
}

// NewHistoryRepository creates a new history repository
func NewHistoryRepository(db *DB) *HistoryRepository {
	return &HistoryRepository{db: db}
}

// Create adds a new history record
func (r *HistoryRepository) Create(history *models.History) error {
	return r.db.Create(history).Error
}

// FindByID finds a history record by ID
func (r *HistoryRepository) FindByID(id uint) (*models.History, error) {
	var history models.History
	err := r.db.First(&history, id).Error
	if err != nil {
		return nil, err
	}
	return &history, nil
}

// FindByMovieID finds all history records for a specific movie
func (r *HistoryRepository) FindByMovieID(movieID string) ([]models.History, error) {
	var history []models.History
	err := r.db.Where("movie_id = ?", movieID).Order("created_at DESC").Find(&history).Error
	return history, err
}

// FindByOperation finds all history records for a specific operation type
func (r *HistoryRepository) FindByOperation(operation string, limit int) ([]models.History, error) {
	var history []models.History
	query := r.db.Where("operation = ?", operation).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	return history, err
}

// FindByStatus finds all history records with a specific status
func (r *HistoryRepository) FindByStatus(status string, limit int) ([]models.History, error) {
	var history []models.History
	query := r.db.Where("status = ?", status).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	return history, err
}

// FindRecent finds the most recent history records
func (r *HistoryRepository) FindRecent(limit int) ([]models.History, error) {
	var history []models.History
	err := r.db.Order("created_at DESC").Limit(limit).Find(&history).Error
	return history, err
}

// FindByDateRange finds history records within a date range
func (r *HistoryRepository) FindByDateRange(start, end time.Time) ([]models.History, error) {
	var history []models.History
	err := r.db.Where("datetime(created_at) BETWEEN datetime(?) AND datetime(?)", start.Format(SqliteTimeFormat), end.Format(SqliteTimeFormat)).Order("created_at DESC").Find(&history).Error
	return history, err
}

// Count returns the total number of history records
func (r *HistoryRepository) Count() (int64, error) {
	var count int64
	err := r.db.Model(&models.History{}).Count(&count).Error
	return count, err
}

// CountByStatus returns the count of records with a specific status
func (r *HistoryRepository) CountByStatus(status string) (int64, error) {
	var count int64
	err := r.db.Model(&models.History{}).Where("status = ?", status).Count(&count).Error
	return count, err
}

// CountByOperation returns the count of records for a specific operation
func (r *HistoryRepository) CountByOperation(operation string) (int64, error) {
	var count int64
	err := r.db.Model(&models.History{}).Where("operation = ?", operation).Count(&count).Error
	return count, err
}

// Delete removes a history record
func (r *HistoryRepository) Delete(id uint) error {
	return r.db.Delete(&models.History{}, id).Error
}

// DeleteByMovieID removes all history records for a specific movie
func (r *HistoryRepository) DeleteByMovieID(movieID string) error {
	return r.db.Where("movie_id = ?", movieID).Delete(&models.History{}).Error
}

// DeleteOlderThan removes history records older than the specified date
func (r *HistoryRepository) DeleteOlderThan(date time.Time) error {
	return r.db.Where("datetime(created_at) < datetime(?)", date.Format(SqliteTimeFormat)).Delete(&models.History{}).Error
}

// List returns a paginated list of history records
func (r *HistoryRepository) List(limit, offset int) ([]models.History, error) {
	var history []models.History
	err := r.db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&history).Error
	return history, err
}

// FindByBatchJobID returns history records for a specific batch job
func (r *HistoryRepository) FindByBatchJobID(batchJobID string) ([]models.History, error) {
	var history []models.History
	err := r.db.Where("batch_job_id = ?", batchJobID).Order("created_at ASC").Find(&history).Error
	return history, err
}

// ActressAliasRepository provides database operations for actress aliases
type ActressAliasRepository struct {
	db *DB
}

// NewActressAliasRepository creates a new actress alias repository
func NewActressAliasRepository(db *DB) *ActressAliasRepository {
	return &ActressAliasRepository{db: db}
}

// Create adds a new actress alias
func (r *ActressAliasRepository) Create(alias *models.ActressAlias) error {
	return r.db.Create(alias).Error
}

// Upsert creates or updates an actress alias
func (r *ActressAliasRepository) Upsert(alias *models.ActressAlias) error {
	existing, err := r.FindByAliasName(alias.AliasName)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// Doesn't exist, create it
		return r.Create(alias)
	}

	// Exists, update it
	alias.ID = existing.ID
	alias.CreatedAt = existing.CreatedAt
	return r.db.Save(alias).Error
}

// FindByAliasName finds a canonical name by alias
func (r *ActressAliasRepository) FindByAliasName(aliasName string) (*models.ActressAlias, error) {
	var alias models.ActressAlias
	err := r.db.First(&alias, "alias_name = ?", aliasName).Error
	if err != nil {
		return nil, err
	}
	return &alias, nil
}

// FindByCanonicalName finds all aliases for a canonical name
func (r *ActressAliasRepository) FindByCanonicalName(canonicalName string) ([]models.ActressAlias, error) {
	var aliases []models.ActressAlias
	err := r.db.Where("canonical_name = ?", canonicalName).Find(&aliases).Error
	return aliases, err
}

// List returns all actress aliases
func (r *ActressAliasRepository) List() ([]models.ActressAlias, error) {
	var aliases []models.ActressAlias
	err := r.db.Find(&aliases).Error
	return aliases, err
}

// Delete removes an actress alias
func (r *ActressAliasRepository) Delete(aliasName string) error {
	return r.db.Delete(&models.ActressAlias{}, "alias_name = ?", aliasName).Error
}

// GetAliasMap returns all aliases as a map[aliasName]canonicalName
func (r *ActressAliasRepository) GetAliasMap() (map[string]string, error) {
	aliases, err := r.List()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, a := range aliases {
		result[a.AliasName] = a.CanonicalName
	}
	return result, nil
}

// MovieTagRepository handles movie tag operations
type MovieTagRepository struct {
	db *DB
}

// NewMovieTagRepository creates a new movie tag repository
func NewMovieTagRepository(db *DB) *MovieTagRepository {
	return &MovieTagRepository{db: db}
}

// AddTag adds a tag to a movie
// Returns error if tag already exists (UNIQUE constraint violation)
func (r *MovieTagRepository) AddTag(movieID, tag string) error {
	movieTag := &models.MovieTag{
		MovieID: movieID,
		Tag:     tag,
	}
	return r.db.Create(movieTag).Error
}

// RemoveTag removes a specific tag from a movie
func (r *MovieTagRepository) RemoveTag(movieID, tag string) error {
	return r.db.Where("movie_id = ? AND tag = ?", movieID, tag).Delete(&models.MovieTag{}).Error
}

// RemoveAllTags removes all tags for a movie
func (r *MovieTagRepository) RemoveAllTags(movieID string) error {
	return r.db.Where("movie_id = ?", movieID).Delete(&models.MovieTag{}).Error
}

// GetTagsForMovie returns all tags for a specific movie
func (r *MovieTagRepository) GetTagsForMovie(movieID string) ([]string, error) {
	var movieTags []models.MovieTag
	err := r.db.Where("movie_id = ?", movieID).Order("tag ASC").Find(&movieTags).Error
	if err != nil {
		return nil, err
	}

	tags := make([]string, len(movieTags))
	for i, mt := range movieTags {
		tags[i] = mt.Tag
	}
	return tags, nil
}

// GetMoviesWithTag returns all movie IDs that have the specified tag
func (r *MovieTagRepository) GetMoviesWithTag(tag string) ([]string, error) {
	var movieTags []models.MovieTag
	err := r.db.Where("tag = ?", tag).Order("movie_id ASC").Find(&movieTags).Error
	if err != nil {
		return nil, err
	}

	movieIDs := make([]string, len(movieTags))
	for i, mt := range movieTags {
		movieIDs[i] = mt.MovieID
	}
	return movieIDs, nil
}

// ListAll returns a map of all movie IDs to their tags
func (r *MovieTagRepository) ListAll() (map[string][]string, error) {
	var movieTags []models.MovieTag
	err := r.db.Order("movie_id ASC, tag ASC").Find(&movieTags).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string][]string)
	for _, mt := range movieTags {
		result[mt.MovieID] = append(result[mt.MovieID], mt.Tag)
	}
	return result, nil
}

// GetUniqueTagsList returns all unique tags in the database
func (r *MovieTagRepository) GetUniqueTagsList() ([]string, error) {
	var tags []string
	err := r.db.Model(&models.MovieTag{}).Distinct("tag").Order("tag ASC").Pluck("tag", &tags).Error
	return tags, err
}

// BatchFileOperationRepository provides database operations for batch file operations
type BatchFileOperationRepository struct {
	db *DB
}

// NewBatchFileOperationRepository creates a new batch file operation repository
func NewBatchFileOperationRepository(db *DB) *BatchFileOperationRepository {
	return &BatchFileOperationRepository{db: db}
}

// Create adds a new batch file operation record
func (r *BatchFileOperationRepository) Create(op *models.BatchFileOperation) error {
	return r.db.Create(op).Error
}

// CreateBatch inserts multiple batch file operation records in a single transaction
func (r *BatchFileOperationRepository) CreateBatch(ops []*models.BatchFileOperation) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, op := range ops {
			if err := tx.Create(op).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// FindByID finds a batch file operation by its primary key
func (r *BatchFileOperationRepository) FindByID(id uint) (*models.BatchFileOperation, error) {
	var op models.BatchFileOperation
	err := r.db.First(&op, id).Error
	if err != nil {
		return nil, err
	}
	return &op, nil
}

// FindByBatchJobID returns all operations for a specific batch job
func (r *BatchFileOperationRepository) FindByBatchJobID(batchJobID string) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.db.Where("batch_job_id = ?", batchJobID).Order("id ASC").Find(&ops).Error
	return ops, err
}

// FindByBatchJobIDAndRevertStatus returns operations filtered by batch job and revert status
func (r *BatchFileOperationRepository) FindByBatchJobIDAndRevertStatus(batchJobID string, revertStatus string) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.db.Where("batch_job_id = ? AND revert_status = ?", batchJobID, revertStatus).Order("id ASC").Find(&ops).Error
	return ops, err
}

// UpdateRevertStatus changes the revert status and sets reverted_at when status is "reverted"
func (r *BatchFileOperationRepository) UpdateRevertStatus(id uint, status string) error {
	updates := map[string]interface{}{
		"revert_status": status,
		"updated_at":    time.Now().UTC(),
	}
	if status == models.RevertStatusReverted {
		updates["reverted_at"] = time.Now().UTC()
	}
	return r.db.Model(&models.BatchFileOperation{}).Where("id = ?", id).Updates(updates).Error
}

// CountByBatchJobID returns the count of operations for a specific batch job
func (r *BatchFileOperationRepository) CountByBatchJobID(batchJobID string) (int64, error) {
	var count int64
	err := r.db.Model(&models.BatchFileOperation{}).Where("batch_job_id = ?", batchJobID).Count(&count).Error
	return count, err
}

// CountByBatchJobIDAndRevertStatus returns the count of operations filtered by batch job and revert status
func (r *BatchFileOperationRepository) CountByBatchJobIDAndRevertStatus(batchJobID string, status string) (int64, error) {
	var count int64
	err := r.db.Model(&models.BatchFileOperation{}).Where("batch_job_id = ? AND revert_status = ?", batchJobID, status).Count(&count).Error
	return count, err
}

// Update persists changes to an existing BatchFileOperation record using GORM Save (upsert).
// Updates all fields including UpdatedAt.
func (r *BatchFileOperationRepository) Update(op *models.BatchFileOperation) error {
	return r.db.Save(op).Error
}

// EventRepository provides database operations for structured event logging
type EventRepository struct {
	db *DB
}

// NewEventRepository creates a new event repository
func NewEventRepository(db *DB) *EventRepository {
	return &EventRepository{db: db}
}

// Create adds a new event record
func (r *EventRepository) Create(event *models.Event) error {
	return r.db.Create(event).Error
}

// FindByID finds an event by its primary key
func (r *EventRepository) FindByID(id uint) (*models.Event, error) {
	var event models.Event
	err := r.db.First(&event, id).Error
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// FindByType returns events matching the given event type, ordered by created_at DESC
func (r *EventRepository) FindByType(eventType string, limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Where("event_type = ?", eventType).Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	return events, err
}

// FindBySeverity returns events matching the given severity, ordered by created_at DESC
func (r *EventRepository) FindBySeverity(severity string, limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Where("severity = ?", severity).Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	return events, err
}

// FindByTypeAndSeverity returns events matching both type and severity, ordered by created_at DESC
func (r *EventRepository) FindByTypeAndSeverity(eventType, severity string, limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Where("event_type = ? AND severity = ?", eventType, severity).Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	return events, err
}

// FindBySource returns events matching the given source, ordered by created_at DESC
func (r *EventRepository) FindBySource(source string, limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Where("source = ?", source).Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	return events, err
}

// FindByDateRange returns events within the time bounds (inclusive start, exclusive end), ordered by created_at DESC
func (r *EventRepository) FindByDateRange(start, end time.Time, limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Where("datetime(created_at) >= datetime(?) AND datetime(created_at) < datetime(?)", start.Format(SqliteTimeFormat), end.Format(SqliteTimeFormat)).Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	return events, err
}

// FindFiltered returns events matching all non-zero filter fields, ordered by created_at DESC
func (r *EventRepository) FindFiltered(filter EventFilter, limit, offset int) ([]models.Event, error) {
	query := r.db.Order("created_at DESC").Limit(limit).Offset(offset)
	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if filter.Start != nil {
		query = query.Where("datetime(created_at) >= datetime(?)", filter.Start.UTC().Format(SqliteTimeFormat))
	}
	if filter.End != nil {
		query = query.Where("datetime(created_at) < datetime(?)", filter.End.UTC().Format(SqliteTimeFormat))
	}
	var events []models.Event
	err := query.Find(&events).Error
	return events, err
}

// CountFiltered returns the count of events matching all non-zero filter fields
func (r *EventRepository) CountFiltered(filter EventFilter) (int64, error) {
	query := r.db.Model(&models.Event{})
	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if filter.Start != nil {
		query = query.Where("datetime(created_at) >= datetime(?)", filter.Start.UTC().Format(SqliteTimeFormat))
	}
	if filter.End != nil {
		query = query.Where("datetime(created_at) < datetime(?)", filter.End.UTC().Format(SqliteTimeFormat))
	}
	var count int64
	err := query.Count(&count).Error
	return count, err
}

// List returns all events ordered by created_at DESC with pagination
func (r *EventRepository) List(limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	return events, err
}

// Count returns the total number of event records
func (r *EventRepository) Count() (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Count(&count).Error
	return count, err
}

// CountByType returns the count of events for a given type
func (r *EventRepository) CountByType(eventType string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Where("event_type = ?", eventType).Count(&count).Error
	return count, err
}

// CountBySeverity returns the count of events for a given severity
func (r *EventRepository) CountBySeverity(severity string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Where("severity = ?", severity).Count(&count).Error
	return count, err
}

func (r *EventRepository) CountByTypeAndSeverity(eventType, severity string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Where("event_type = ? AND severity = ?", eventType, severity).Count(&count).Error
	return count, err
}

// CountBySource returns the count of events for a given source
func (r *EventRepository) CountBySource(source string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Where("source = ?", source).Count(&count).Error
	return count, err
}

// CountGroupBySource returns event counts grouped by source
func (r *EventRepository) CountGroupBySource() (map[string]int64, error) {
	type result struct {
		Source string
		Count  int64
	}
	var results []result
	err := r.db.Model(&models.Event{}).Select("source, count(*) as count").Group("source").Find(&results).Error
	if err != nil {
		return nil, err
	}
	bySource := make(map[string]int64, len(results))
	for _, r := range results {
		bySource[r.Source] = r.Count
	}
	return bySource, nil
}

func (r *EventRepository) CountByDateRange(start, end time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Where("datetime(created_at) >= datetime(?) AND datetime(created_at) < datetime(?)", start.Format(SqliteTimeFormat), end.Format(SqliteTimeFormat)).Count(&count).Error
	return count, err
}

// DeleteOlderThan removes events before the cutoff date (for event table cleanup)
func (r *EventRepository) DeleteOlderThan(date time.Time) error {
	return r.db.Where("datetime(created_at) < datetime(?)", date.Format(SqliteTimeFormat)).Delete(&models.Event{}).Error
}
