package template

import (
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/mediainfo"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Context holds all data available for template execution
type Context struct {
	// Basic identifiers
	ID        string
	ContentID string

	// Title information
	Title         string
	OriginalTitle string // Japanese/original language title

	// Date information
	ReleaseDate *time.Time
	Runtime     int // in minutes

	// People
	Director  string
	Actresses []string // Array of actress names
	FirstName string   // For single actress context
	LastName  string   // For single actress context

	// Production info
	Maker  string // Studio/Maker
	Label  string
	Series string

	// Categories
	Genres []string

	// Media info
	OriginalFilename string
	VideoFilePath    string // Path to video file for mediainfo extraction

	// Indexing (for screenshots, multi-part, etc.)
	Index int

	// Multi-part file information
	PartNumber  int    // Part number (1, 2, 3, etc.) - 0 means single file
	PartSuffix  string // Original part suffix detected from filename (e.g., "-pt1", "-A")
	IsMultiPart bool   // Whether this is a multi-part file

	// Cached mediainfo (lazy-loaded)
	cachedMediaInfo *mediainfo.VideoInfo
	mediaInfoError  error // Cached error to avoid repeated analysis failures

	// Additional metadata
	Rating      float64
	Description string
	CoverURL    string
	TrailerURL  string

	// Translations keyed by normalized language code (e.g. "en", "ja")
	// IMMUTABLE after construction - safe for concurrent read access
	Translations map[string]models.MovieTranslation

	// Optional per-context override for rendered language preference
	// When empty, Engine default language is used
	// IMPORTANT: Setting this changes behavior of unqualified tags like <TITLE>
	DefaultLanguage string

	// Output configuration
	GroupActress bool // Replace multiple actresses with "@Group"
}

// NewContextFromMovie creates a template context from a Movie model
func NewContextFromMovie(movie *models.Movie) *Context {
	ctx := &Context{
		ID:               movie.ID,
		ContentID:        movie.ContentID,
		Title:            movie.Title,
		OriginalTitle:    movie.OriginalTitle,
		ReleaseDate:      movie.ReleaseDate,
		Runtime:          movie.Runtime,
		Director:         movie.Director,
		Maker:            movie.Maker,
		Label:            movie.Label,
		Series:           movie.Series,
		OriginalFilename: movie.OriginalFileName,
		Description:      movie.Description,
		CoverURL:         movie.CoverURL,
		TrailerURL:       movie.TrailerURL,
		Translations:     buildTranslationMap(movie.Translations),
	}

	// Extract rating
	if movie.RatingScore > 0 {
		ctx.Rating = movie.RatingScore
	}

	// Build actress list
	if len(movie.Actresses) > 0 {
		ctx.Actresses = make([]string, 0, len(movie.Actresses))
		for _, actress := range movie.Actresses {
			ctx.Actresses = append(ctx.Actresses, actress.FullName())
		}

		// Set first/last name from first actress for single-actress templates
		if len(movie.Actresses) > 0 {
			ctx.FirstName = movie.Actresses[0].FirstName
			ctx.LastName = movie.Actresses[0].LastName
		}
	}

	// Build genre list
	if len(movie.Genres) > 0 {
		ctx.Genres = make([]string, 0, len(movie.Genres))
		for _, genre := range movie.Genres {
			ctx.Genres = append(ctx.Genres, genre.Name)
		}
	}

	return ctx
}

// NewContextFromScraperResult creates a template context from a ScraperResult
func NewContextFromScraperResult(result *models.ScraperResult) *Context {
	ctx := &Context{
		ID:            result.ID,
		ContentID:     result.ContentID,
		Title:         result.Title,
		OriginalTitle: result.OriginalTitle,
		ReleaseDate:   result.ReleaseDate,
		Runtime:       result.Runtime,
		Director:      result.Director,
		Maker:         result.Maker,
		Label:         result.Label,
		Series:        result.Series,
		Description:   result.Description,
		CoverURL:      result.CoverURL,
		TrailerURL:    result.TrailerURL,
		Translations:  map[string]models.MovieTranslation{},
	}

	// Extract rating
	if result.Rating != nil {
		ctx.Rating = result.Rating.Score
	}

	// Build actress list
	if len(result.Actresses) > 0 {
		ctx.Actresses = make([]string, 0, len(result.Actresses))
		for _, actress := range result.Actresses {
			ctx.Actresses = append(ctx.Actresses, actress.FullName())
		}

		// Set first/last name from first actress
		if len(result.Actresses) > 0 {
			ctx.FirstName = result.Actresses[0].FirstName
			ctx.LastName = result.Actresses[0].LastName
		}
	}

	// Build genre list
	ctx.Genres = result.Genres

	return ctx
}

// Clone creates a copy of the context
func (c *Context) Clone() *Context {
	clone := *c

	// Deep copy slices
	if c.Actresses != nil {
		clone.Actresses = make([]string, len(c.Actresses))
		copy(clone.Actresses, c.Actresses)
	}

	if c.Genres != nil {
		clone.Genres = make([]string, len(c.Genres))
		copy(clone.Genres, c.Genres)
	}

	if c.Translations != nil {
		clone.Translations = make(map[string]models.MovieTranslation, len(c.Translations))
		for k, v := range c.Translations {
			clone.Translations[k] = v
		}
	}

	return &clone
}

// GetMediaInfo lazy-loads and caches video metadata.
// Caches both success and failure states to avoid repeated expensive analysis.
func (c *Context) GetMediaInfo() *mediainfo.VideoInfo {
	if c.cachedMediaInfo != nil {
		return c.cachedMediaInfo
	}

	// Return cached failure (nil result already cached)
	if c.mediaInfoError != nil {
		return nil
	}

	if c.VideoFilePath == "" {
		c.mediaInfoError = fmt.Errorf("no video file path")
		return nil
	}

	// Analyze video file
	info, err := mediainfo.Analyze(c.VideoFilePath)
	if err != nil {
		c.mediaInfoError = err
		return nil
	}

	c.cachedMediaInfo = info
	return info
}

// buildTranslationMap creates a language-keyed map from translation records.
// Input MUST be deterministically ordered (e.g., by language ASC) to ensure
// consistent "first wins" behavior for duplicate languages.
func buildTranslationMap(translations []models.MovieTranslation) map[string]models.MovieTranslation {
	if len(translations) == 0 {
		return map[string]models.MovieTranslation{}
	}

	m := make(map[string]models.MovieTranslation, len(translations))
	for _, translation := range translations {
		lang := normalizeLanguageCode(translation.Language)
		if lang == "" {
			continue
		}

		// Keep first non-empty translation for a language
		// Deterministic because input is ordered
		if _, exists := m[lang]; !exists {
			m[lang] = translation
		}
	}

	return m
}

// normalizeLanguageCode normalizes language codes to base language only.
// This is LOSSY: "en-US" becomes "en", "zh-Hant" becomes "zh".
// Returns empty string for invalid codes (including 3-letter ISO 639-2 codes like "eng", "jpn").
func normalizeLanguageCode(lang string) string {
	lang = strings.TrimSpace(strings.ToLower(lang))
	if lang == "" {
		return ""
	}

	// Normalize separators and drop region/script suffixes
	lang = strings.ReplaceAll(lang, "_", "-")
	if idx := strings.Index(lang, "-"); idx > 0 {
		lang = lang[:idx]
	}

	// Validate: must be 2-letter alphabetic code
	if len(lang) != 2 || lang[0] < 'a' || lang[0] > 'z' || lang[1] < 'a' || lang[1] > 'z' {
		return ""
	}

	return lang
}
