package database

import (
	"fmt"
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
}

// New creates a new database connection
func New(cfg *config.Config) (*DB, error) {
	var dialector gorm.Dialector

	switch cfg.Database.Type {
	case "sqlite", "":
		dialector = sqlite.Open(cfg.Database.DSN)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Database.Type)
	}

	// Configure logger level
	logLevel := logger.Silent
	switch cfg.Logging.Level {
	case "debug":
		logLevel = logger.Info
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &DB{db}, nil
}

// AutoMigrate runs database migrations
func (db *DB) AutoMigrate() error {
	return db.DB.AutoMigrate(
		&models.Movie{},
		&models.MovieTranslation{},
		&models.Actress{},
		&models.Genre{},
		&models.GenreReplacement{},
		&models.ActressAlias{},
		&models.MovieTag{},
		&models.History{},
		&models.ContentIDMapping{},
	)
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
	// Check if movie exists
	existing, err := r.FindByID(movie.ID)
	if err != nil {
		// Movie doesn't exist, create it with associations
		// First, ensure genres and actresses exist or get their IDs
		if err := r.ensureGenresExist(movie.Genres); err != nil {
			return err
		}
		if err := r.ensureActressesExist(movie.Actresses); err != nil {
			return err
		}
		return r.db.Create(movie).Error
	}

	// Movie exists, update it
	// Preserve the CreatedAt timestamp from the existing record
	movie.CreatedAt = existing.CreatedAt

	// Set the MovieID for all translations
	for i := range movie.Translations {
		movie.Translations[i].MovieID = movie.ID
	}

	// Ensure genres and actresses exist
	if err := r.ensureGenresExist(movie.Genres); err != nil {
		return err
	}
	if err := r.ensureActressesExist(movie.Actresses); err != nil {
		return err
	}

	// Save translations separately - temporarily remove them to avoid Save() trying to insert them
	translations := movie.Translations
	movie.Translations = nil

	// Update the movie record (without translations)
	if err := r.db.Save(movie).Error; err != nil {
		return err
	}

	// Replace associations
	if err := r.db.Model(movie).Association("Genres").Replace(movie.Genres); err != nil {
		return err
	}
	if err := r.db.Model(movie).Association("Actresses").Replace(movie.Actresses); err != nil {
		return err
	}

	// Upsert translations individually to avoid UNIQUE constraint violations
	translationRepo := NewMovieTranslationRepository(r.db)
	for i := range translations {
		translations[i].MovieID = movie.ID
		if err := translationRepo.Upsert(&translations[i]); err != nil {
			return err
		}
	}

	// Restore translations to movie object
	movie.Translations = translations

	return nil
}

// ensureGenresExist ensures all genres exist in DB, gets or creates them
func (r *MovieRepository) ensureGenresExist(genres []models.Genre) error {
	for i := range genres {
		var existing models.Genre
		err := r.db.Where("name = ?", genres[i].Name).First(&existing).Error
		if err == nil {
			// Genre exists, use its ID
			genres[i] = existing
		} else {
			// Genre doesn't exist, create it
			if err := r.db.Create(&genres[i]).Error; err != nil {
				// Might be a race condition, try to find it again
				if err := r.db.Where("name = ?", genres[i].Name).First(&existing).Error; err == nil {
					genres[i] = existing
				} else {
					return err
				}
			}
		}
	}
	return nil
}

// ensureActressesExist ensures all actresses exist in DB, gets or creates them
func (r *MovieRepository) ensureActressesExist(actresses []models.Actress) error {
	for i := range actresses {
		var existing models.Actress
		var err error

		// Try to find actress using DMMID if available (primary unique identifier)
		if actresses[i].DMMID != 0 {
			err = r.db.Where("dmm_id = ?", actresses[i].DMMID).First(&existing).Error
		} else if actresses[i].JapaneseName != "" {
			// Fall back to japanese_name if DMMID is not available
			err = r.db.Where("japanese_name = ?", actresses[i].JapaneseName).First(&existing).Error
		} else if actresses[i].FirstName != "" || actresses[i].LastName != "" {
			// Fall back to first_name and last_name if neither DMMID nor japanese_name is available
			err = r.db.Where("first_name = ? AND last_name = ?", actresses[i].FirstName, actresses[i].LastName).First(&existing).Error
		} else {
			// Skip actresses with no identifying information
			continue
		}

		if err == nil {
			// Actress exists, use their ID
			actresses[i] = existing
		} else {
			// Actress doesn't exist, create them
			if err := r.db.Create(&actresses[i]).Error; err != nil {
				// Might be a race condition, try to find again
				if actresses[i].DMMID != 0 {
					if err := r.db.Where("dmm_id = ?", actresses[i].DMMID).First(&existing).Error; err == nil {
						actresses[i] = existing
						continue
					}
				} else if actresses[i].JapaneseName != "" {
					if err := r.db.Where("japanese_name = ?", actresses[i].JapaneseName).First(&existing).Error; err == nil {
						actresses[i] = existing
						continue
					}
				} else if actresses[i].FirstName != "" || actresses[i].LastName != "" {
					if err := r.db.Where("first_name = ? AND last_name = ?", actresses[i].FirstName, actresses[i].LastName).First(&existing).Error; err == nil {
						actresses[i] = existing
						continue
					}
				}
				return err
			}
		}
	}
	return nil
}

// FindByID finds a movie by its ID
func (r *MovieRepository) FindByID(id string) (*models.Movie, error) {
	var movie models.Movie
	err := r.db.Preload("Actresses").Preload("Genres").Preload("Translations").First(&movie, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &movie, nil
}

// FindByContentID finds a movie by its content ID
func (r *MovieRepository) FindByContentID(contentID string) (*models.Movie, error) {
	var movie models.Movie
	err := r.db.Preload("Actresses").Preload("Genres").First(&movie, "content_id = ?", contentID).Error
	if err != nil {
		return nil, err
	}
	return &movie, nil
}

// Delete deletes a movie by ID
func (r *MovieRepository) Delete(id string) error {
	// Delete translations first (foreign key constraint)
	if err := r.db.Delete(&models.MovieTranslation{}, "movie_id = ?", id).Error; err != nil {
		return err
	}
	// Then delete the movie
	return r.db.Delete(&models.Movie{}, "id = ?", id).Error
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
	err := r.db.Limit(limit).Offset(offset).Find(&actresses).Error
	return actresses, err
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
	// Try to find existing translation for this movie and language
	existing, err := r.FindByMovieAndLanguage(translation.MovieID, translation.Language)
	if err != nil {
		// Translation doesn't exist, create it
		return r.db.Create(translation).Error
	}

	// Translation exists, update it
	translation.ID = existing.ID
	translation.CreatedAt = existing.CreatedAt
	return r.db.Save(translation).Error
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
	err := r.db.Where("created_at BETWEEN ? AND ?", start, end).Order("created_at DESC").Find(&history).Error
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
	return r.db.Where("created_at < ?", date).Delete(&models.History{}).Error
}

// List returns a paginated list of history records
func (r *HistoryRepository) List(limit, offset int) ([]models.History, error) {
	var history []models.History
	err := r.db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&history).Error
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
