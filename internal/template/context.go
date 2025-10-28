package template

import (
	"time"

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

	// Indexing (for screenshots, multi-part, etc.)
	Index int

	// Additional metadata
	Rating      float64
	Description string
	CoverURL    string
	TrailerURL  string
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

	return &clone
}
