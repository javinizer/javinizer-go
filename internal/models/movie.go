package models

import (
	"time"
)

// Movie represents the aggregated metadata for a JAV movie
type Movie struct {
	ContentID        string     `json:"content_id" gorm:"primaryKey"`
	ID               string     `json:"id" gorm:"index"`
	DisplayTitle     string     `json:"display_title"`
	Title            string     `json:"title"`
	OriginalTitle    string     `json:"original_title"` // Japanese/original language title
	Description      string     `json:"description" gorm:"type:text"`
	ReleaseDate      *time.Time `json:"release_date"`
	ReleaseYear      int        `json:"release_year"`
	Runtime          int        `json:"runtime"` // in minutes
	Director         string     `json:"director"`
	Maker            string     `json:"maker"`  // Studio/maker
	Label            string     `json:"label"`  // Sub-label
	Series           string     `json:"series"` // Series name
	RatingScore      float64    `json:"rating_score" gorm:"column:rating_score"`
	RatingVotes      int        `json:"rating_votes" gorm:"column:rating_votes"`
	PosterURL        string     `json:"poster_url"`         // Portrait/box art image
	CoverURL         string     `json:"cover_url"`          // Landscape/fanart image
	CroppedPosterURL string     `json:"cropped_poster_url"` // URL to the cropped poster (persisted)
	ShouldCropPoster bool       `json:"should_crop_poster"` // Whether poster needs cropping from cover
	TrailerURL       string     `json:"trailer_url"`
	OriginalFileName string     `json:"original_filename"`

	// Relationships
	Actresses   []Actress `json:"actresses" gorm:"many2many:movie_actresses;foreignKey:ContentID;joinForeignKey:MovieContentID;References:ID;joinReferences:ActressID"`
	Genres      []Genre   `json:"genres" gorm:"many2many:movie_genres;foreignKey:ContentID;joinForeignKey:MovieContentID;References:ID;joinReferences:GenreID"`
	Screenshots []string  `json:"screenshot_urls" gorm:"serializer:json"`

	// Translations
	Translations []MovieTranslation `json:"translations" gorm:"foreignKey:MovieID;references:ContentID"`

	// Metadata
	SourceName string    `json:"source_name"` // Primary source
	SourceURL  string    `json:"source_url"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// MovieTranslation represents a movie's metadata in a specific language
type MovieTranslation struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	MovieID       string    `json:"movie_id" gorm:"index:idx_movie_language,unique"`
	Language      string    `json:"language" gorm:"index:idx_movie_language,unique;size:5"` // ISO 639-1: en, ja, zh, etc.
	Title         string    `json:"title"`
	OriginalTitle string    `json:"original_title"` // Japanese/original language title
	Description   string    `json:"description" gorm:"type:text"`
	Director      string    `json:"director"`
	Maker         string    `json:"maker"`
	Label         string    `json:"label"`
	Series        string    `json:"series"`
	SourceName    string    `json:"source_name"`                           // Which scraper provided this translation
	SettingsHash  string    `gorm:"type:varchar(16)" json:"settings_hash"` // Hash of translation settings used
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Actress represents a JAV actress
type Actress struct {
	ID           uint   `json:"id" gorm:"primaryKey"`
	DMMID        int    `json:"dmm_id"` // Real DMM actress ID when available (unique only for values > 0)
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	JapaneseName string `json:"japanese_name" gorm:"index"`
	ThumbURL     string `json:"thumb_url"`
	Aliases      string `json:"aliases"` // Pipe-separated

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FullName returns the actress's full English name
func (a *Actress) FullName() string {
	if a.LastName != "" && a.FirstName != "" {
		return a.LastName + " " + a.FirstName
	}
	if a.FirstName != "" {
		return a.FirstName
	}
	return a.JapaneseName
}

// Genre represents a category/tag
type Genre struct {
	ID   uint   `json:"id" gorm:"primaryKey"`
	Name string `json:"name" gorm:"uniqueIndex"`
}

// GenreReplacement represents a user-defined genre name mapping
type GenreReplacement struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Original    string    `json:"original" gorm:"uniqueIndex;not null"`
	Replacement string    `json:"replacement" gorm:"not null"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ActressAlias represents an alternate name mapping for an actress
// This allows users to consolidate multiple actress names into a canonical one
type ActressAlias struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	AliasName     string    `json:"alias_name" gorm:"uniqueIndex;not null"` // The alternate name (e.g., "Yui Hatano")
	CanonicalName string    `json:"canonical_name" gorm:"index;not null"`   // The canonical/preferred name (e.g., "Hatano Yui")
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// MovieTag represents a custom user-defined tag for a specific movie
// Tags are used for personal organization and appear in NFO files
type MovieTag struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	MovieID   string    `json:"movie_id" gorm:"index:idx_movie_tag,unique;not null;size:50"` // Foreign key to movies.content_id (CASCADE handled in Delete)
	Tag       string    `json:"tag" gorm:"index:idx_movie_tag,unique;not null;size:100"`     // Tag name (case-sensitive)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName specifies the table name for Movie
func (Movie) TableName() string {
	return "movies"
}

// TableName specifies the table name for MovieTranslation
func (MovieTranslation) TableName() string {
	return "movie_translations"
}

// TableName specifies the table name for Actress
func (Actress) TableName() string {
	return "actresses"
}

// TableName specifies the table name for Genre
func (Genre) TableName() string {
	return "genres"
}

// TableName specifies the table name for MovieTag
func (MovieTag) TableName() string {
	return "movie_tags"
}

// History represents a log of file organization operations
type History struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	MovieID      string    `json:"movie_id" gorm:"index"`          // Foreign key to movies.content_id (nullable for historical records)
	BatchJobID   *string   `json:"batch_job_id" gorm:"index"`      // Foreign key to jobs.id (nullable: historical records have no batch job)
	Operation    string    `json:"operation"`                      // "scrape", "organize", "download", "nfo"
	OriginalPath string    `json:"original_path"`                  // Source file path
	NewPath      string    `json:"new_path"`                       // Destination file path
	Status       string    `json:"status"`                         // "success", "failed", "reverted"
	ErrorMessage string    `json:"error_message" gorm:"type:text"` // Error details if failed
	Metadata     string    `json:"metadata" gorm:"type:json"`      // Additional metadata (JSON)
	DryRun       bool      `json:"dry_run"`                        // Whether this was a dry run
	CreatedAt    time.Time `json:"created_at" gorm:"index"`
}

// TableName specifies the table name for History
func (History) TableName() string {
	return "history"
}
