package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// History typed enums — type X string for GORM compatibility with existing string columns
// ---------------------------------------------------------------------------

// HistoryOperation represents the type of operation recorded in history.
type HistoryOperation string

const (
	HistoryOpScrape   HistoryOperation = "scrape"
	HistoryOpOrganize HistoryOperation = "organize"
	HistoryOpDownload HistoryOperation = "download"
	HistoryOpNFO      HistoryOperation = "nfo"
)

func (e HistoryOperation) String() string { return string(e) }

func (e HistoryOperation) MarshalJSON() ([]byte, error)  { return MarshalStringEnum(string(e)) }
func (e *HistoryOperation) UnmarshalJSON(b []byte) error { return UnmarshalStringEnum((*string)(e), b) }

func (e *HistoryOperation) Scan(value any) error        { return ScanStringEnum((*string)(e), value) }
func (e HistoryOperation) Value() (driver.Value, error) { return StringEnumValue(string(e)) }

// HistoryStatus represents the outcome status of a history record.
type HistoryStatus string

const (
	HistoryStatusSuccess  HistoryStatus = "success"
	HistoryStatusFailed   HistoryStatus = "failed"
	HistoryStatusReverted HistoryStatus = "reverted"
)

func (e HistoryStatus) String() string { return string(e) }

func (e HistoryStatus) MarshalJSON() ([]byte, error)  { return MarshalStringEnum(string(e)) }
func (e *HistoryStatus) UnmarshalJSON(b []byte) error { return UnmarshalStringEnum((*string)(e), b) }

func (e *HistoryStatus) Scan(value any) error        { return ScanStringEnum((*string)(e), value) }
func (e HistoryStatus) Value() (driver.Value, error) { return StringEnumValue(string(e)) }

// Movie represents the aggregated metadata for a JAV movie
type Movie struct {
	ContentID        string      `json:"content_id" gorm:"primaryKey"`
	ID               string      `json:"id" gorm:"index"`
	DisplayTitle     string      `json:"display_title"`
	Title            string      `json:"title"`
	OriginalTitle    string      `json:"original_title"` // Japanese/original language title
	Description      string      `json:"description" gorm:"type:text"`
	ReleaseDate      *time.Time  `json:"release_date"`
	ReleaseYear      int         `json:"release_year"`
	Runtime          int         `json:"runtime"` // in minutes
	Director         string      `json:"director"`
	Maker            string      `json:"maker"`  // Studio/maker
	Label            string      `json:"label"`  // Sub-label
	Series           string      `json:"series"` // Series name
	RatingScore      float64     `json:"rating_score" gorm:"column:rating_score"`
	RatingVotes      int         `json:"rating_votes" gorm:"column:rating_votes"`
	RatingWarning    string      `json:"rating_warning,omitempty" gorm:"-"`
	Poster           PosterState `json:"-" gorm:"embedded"`
	TrailerURL       string      `json:"trailer_url"`
	OriginalFileName string      `json:"original_filename"`

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

	// Translations carries optional translation data for API response enrichment.
	// Not persisted in database — gorm:"-" prevents auto-migration/association.
	Translations []ActressTranslation `json:"translations,omitempty" gorm:"-"`
}

// FullName returns the actress's full English name
func (a *Actress) FullName() string {
	return formatActressNameSimple(a.LastName, a.FirstName, a.JapaneseName)
}

// Genre represents a category/tag
type Genre struct {
	ID   uint   `json:"id" gorm:"primaryKey"`
	Name string `json:"name" gorm:"uniqueIndex"`

	// Translations carries optional translation data for API response enrichment.
	// Not persisted in database — gorm:"-" prevents auto-migration/association.
	Translations []GenreTranslation `json:"translations,omitempty" gorm:"-"`
}

// GenreTranslation represents a translated genre name in a specific language
type GenreTranslation struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	GenreID    uint      `json:"genre_id" gorm:"index:idx_genre_translation_genre_language,unique;not null"`
	Language   string    `json:"language" gorm:"index:idx_genre_translation_genre_language,unique;size:5;not null"`
	Name       string    `json:"name"`
	SourceName string    `json:"source_name"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TableName specifies the table name for GenreTranslation
func (GenreTranslation) TableName() string {
	return "genre_translations"
}

// ActressTranslation represents a translated actress name in a specific language
type ActressTranslation struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	ActressID    uint      `json:"actress_id" gorm:"index:idx_actress_translation_actress_language,unique;not null"`
	Language     string    `json:"language" gorm:"index:idx_actress_translation_actress_language,unique;size:5;not null"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	JapaneseName string    `json:"japanese_name"`
	DisplayName  string    `json:"display_name"`
	SourceName   string    `json:"source_name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName specifies the table name for ActressTranslation
func (ActressTranslation) TableName() string {
	return "actress_translations"
}

// GenreTranslationData carries genre translation without a genre_id (resolved later by caller).
type GenreTranslationData struct {
	GenreIndex int // Index into movie.Genres for ID resolution
	Language   string
	Name       string
	SourceName string
}

// ActressTranslationData carries actress translation without an actress_id (resolved later by caller).
type ActressTranslationData struct {
	ActressIndex int // Index into movie.Actresses for ID resolution
	Language     string
	FirstName    string
	LastName     string
	JapaneseName string
	DisplayName  string
	SourceName   string
}

// GenreReplacement represents a user-defined genre name mapping
type GenreReplacement struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Original    string    `json:"original" gorm:"uniqueIndex;not null"`
	Replacement string    `json:"replacement" gorm:"not null"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WordReplacement represents a user-defined text string mapping for uncensoring metadata
type WordReplacement struct {
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

// Clone returns a deep copy of the Movie.
// Adding a field to Movie requires updating this method in the same change.
func (m *Movie) Clone() *Movie {
	if m == nil {
		return nil
	}
	clone := *m
	clone.Poster = m.Poster.Clone()
	// Deep-copy pointer fields
	if m.ReleaseDate != nil {
		t := *m.ReleaseDate
		clone.ReleaseDate = &t
	}
	// Deep-copy slice fields
	if m.Actresses != nil {
		clone.Actresses = make([]Actress, len(m.Actresses))
		copy(clone.Actresses, m.Actresses)
		for i := range clone.Actresses {
			if m.Actresses[i].Translations != nil {
				clone.Actresses[i].Translations = make([]ActressTranslation, len(m.Actresses[i].Translations))
				copy(clone.Actresses[i].Translations, m.Actresses[i].Translations)
			}
		}
	}
	if m.Genres != nil {
		clone.Genres = make([]Genre, len(m.Genres))
		copy(clone.Genres, m.Genres)
		for i := range clone.Genres {
			if m.Genres[i].Translations != nil {
				clone.Genres[i].Translations = make([]GenreTranslation, len(m.Genres[i].Translations))
				copy(clone.Genres[i].Translations, m.Genres[i].Translations)
			}
		}
	}
	if m.Screenshots != nil {
		clone.Screenshots = make([]string, len(m.Screenshots))
		copy(clone.Screenshots, m.Screenshots)
	}
	// NOTE: copy() is a shallow element copy. This is safe as long as the element
	// types (MovieTranslation, GenreTranslationData, ActressTranslationData)
	// contain only value types. If pointer or slice fields are added to these
	// types in the future, deep-copy those fields here like Genres.Translations
	// above.
	if m.Translations != nil {
		clone.Translations = make([]MovieTranslation, len(m.Translations))
		copy(clone.Translations, m.Translations)
	}
	return &clone
}

// MarshalJSON flattens PosterState fields into Movie JSON output, preserving the
// pre-extraction flat wire format. Without this, json:"-" on Poster would cause all
// poster fields to disappear from API responses.
func (m Movie) MarshalJSON() ([]byte, error) {
	type Alias Movie
	aux := &struct {
		PosterURL                string `json:"poster_url"`
		CoverURL                 string `json:"cover_url"`
		CroppedPosterURL         string `json:"cropped_poster_url"`
		ShouldCropPoster         bool   `json:"should_crop_poster"`
		OriginalPosterURL        string `json:"original_poster_url"`
		OriginalCroppedPosterURL string `json:"original_cropped_poster_url"`
		OriginalShouldCropPoster *bool  `json:"original_should_crop_poster"`
		*Alias
	}{
		PosterURL:                m.Poster.PosterURL,
		CoverURL:                 m.Poster.CoverURL,
		CroppedPosterURL:         m.Poster.CroppedPosterURL,
		ShouldCropPoster:         m.Poster.ShouldCropPoster,
		OriginalPosterURL:        m.Poster.OriginalPosterURL,
		OriginalCroppedPosterURL: m.Poster.OriginalCroppedPosterURL,
		OriginalShouldCropPoster: m.Poster.OriginalShouldCropPoster,
		Alias:                    (*Alias)(&m),
	}
	return json.Marshal(aux)
}

// UnmarshalJSON populates PosterState from flat JSON fields, preserving the
// pre-extraction flat wire format. Required inverse of MarshalJSON.
func (m *Movie) UnmarshalJSON(data []byte) error {
	type Alias Movie
	aux := &struct {
		PosterURL                string `json:"poster_url"`
		CoverURL                 string `json:"cover_url"`
		CroppedPosterURL         string `json:"cropped_poster_url"`
		ShouldCropPoster         bool   `json:"should_crop_poster"`
		OriginalPosterURL        string `json:"original_poster_url"`
		OriginalCroppedPosterURL string `json:"original_cropped_poster_url"`
		OriginalShouldCropPoster *bool  `json:"original_should_crop_poster"`
		*Alias
	}{
		Alias: (*Alias)(m),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	m.Poster = PosterState{
		PosterURL:                aux.PosterURL,
		CoverURL:                 aux.CoverURL,
		CroppedPosterURL:         aux.CroppedPosterURL,
		ShouldCropPoster:         aux.ShouldCropPoster,
		OriginalPosterURL:        aux.OriginalPosterURL,
		OriginalCroppedPosterURL: aux.OriginalCroppedPosterURL,
		OriginalShouldCropPoster: aux.OriginalShouldCropPoster,
	}
	return nil
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
	ID           uint             `json:"id" gorm:"primaryKey"`
	MovieID      string           `json:"movie_id" gorm:"index"`
	BatchJobID   *string          `json:"batch_job_id" gorm:"index"`
	Operation    HistoryOperation `json:"operation"`
	OriginalPath string           `json:"original_path"`
	NewPath      string           `json:"new_path"`
	Status       HistoryStatus    `json:"status"`
	ErrorMessage string           `json:"error_message" gorm:"type:text"`
	Metadata     string           `json:"metadata" gorm:"type:json"`
	DryRun       bool             `json:"dry_run"`
	CreatedAt    time.Time        `json:"created_at" gorm:"index"`
}

// TableName specifies the table name for History
func (History) TableName() string {
	return "history"
}
