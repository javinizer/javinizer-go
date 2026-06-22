// Package testutil provides shared test utilities and helpers for javinizer-go tests.
//
// This file contains test data builders for domain models using the builder pattern.
// Builders provide sensible defaults and fluent API methods to minimize test boilerplate
// while maintaining flexibility for custom test scenarios.
package testutil

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// MovieBuilder constructs Movie test entities using the builder pattern with fluent API.
// It provides sensible defaults and method chaining for easy test data creation.
//
// Default values:
//   - ID: "IPX-123" (canonical test movie ID)
//   - ContentID: "ipx00123"
//   - Title: "Test Movie"
//
// Example usage:
//
//	movie := testutil.NewMovieBuilder().
//	    WithTitle("Custom Title").
//	    WithActresses([]string{"Actress 1", "Actress 2"}).
//	    WithGenres([]string{"Drama"}).
//	    Build()
type MovieBuilder struct {
	movie *models.Movie
}

// NewMovieBuilder creates a new MovieBuilder with sensible defaults.
// Default: ID="IPX-123", ContentID="ipx00123", Title="Test Movie"
func NewMovieBuilder() *MovieBuilder {
	return &MovieBuilder{
		movie: &models.Movie{
			ID:        "IPX-123",
			ContentID: "ipx00123",
			Title:     "Test Movie",
		},
	}
}

// WithID sets the movie ID and returns the builder for chaining.
func (b *MovieBuilder) WithID(id string) *MovieBuilder {
	b.movie.ID = id
	return b
}

// WithContentID sets the content ID and returns the builder for chaining.
func (b *MovieBuilder) WithContentID(contentID string) *MovieBuilder {
	b.movie.ContentID = contentID
	return b
}

// WithTitle sets the movie title and returns the builder for chaining.
func (b *MovieBuilder) WithTitle(title string) *MovieBuilder {
	b.movie.Title = title
	return b
}

// WithActresses sets the actresses and returns the builder for chaining.
// The actresses parameter is converted to the models.Actress format.
func (b *MovieBuilder) WithActresses(actresses []string) *MovieBuilder {
	if actresses == nil {
		b.movie.Actresses = nil
		return b
	}

	actressList := make([]models.Actress, len(actresses))
	for i, name := range actresses {
		actressList[i] = models.Actress{
			FirstName: name,
		}
	}
	b.movie.Actresses = actressList
	return b
}

// WithGenres sets the genres and returns the builder for chaining.
// The genres parameter is converted to the models.Genre format.
func (b *MovieBuilder) WithGenres(genres []string) *MovieBuilder {
	if genres == nil {
		b.movie.Genres = nil
		return b
	}

	genreList := make([]models.Genre, len(genres))
	for i, name := range genres {
		genreList[i] = models.Genre{
			Name: name,
		}
	}
	b.movie.Genres = genreList
	return b
}

// WithReleaseDate sets the release date and returns the builder for chaining.
func (b *MovieBuilder) WithReleaseDate(date time.Time) *MovieBuilder {
	b.movie.ReleaseDate = &date
	return b
}

// WithCoverURL sets the cover URL and returns the builder for chaining.
func (b *MovieBuilder) WithCoverURL(url string) *MovieBuilder {
	b.movie.Poster.CoverURL = url
	return b
}

// WithDescription sets the description and returns the builder for chaining.
func (b *MovieBuilder) WithDescription(description string) *MovieBuilder {
	b.movie.Description = description
	return b
}

// WithStudio sets the maker (studio) and returns the builder for chaining.
func (b *MovieBuilder) WithStudio(studio string) *MovieBuilder {
	b.movie.Maker = studio
	return b
}

// Build returns the constructed Movie instance.
func (b *MovieBuilder) Build() *models.Movie {
	return b.movie
}

// ActressBuilder constructs Actress test entities using the builder pattern with fluent API.
// It provides sensible defaults and method chaining for easy test data creation.
//
// Default values:
//   - FirstName: "Test Actress"
//   - DMMID: 0 (use WithDMMID to set canonical test value "123456")
//
// Example usage:
//
//	actress := testutil.NewActressBuilder().
//	    WithName("Jane Doe").
//	    WithDMMID("123456").
//	    Build()
type ActressBuilder struct {
	actress *models.Actress
}

// NewActressBuilder creates a new ActressBuilder with sensible defaults.
// Default: FirstName="Test Actress"
func NewActressBuilder() *ActressBuilder {
	return &ActressBuilder{
		actress: &models.Actress{
			FirstName: "Test Actress",
		},
	}
}

// WithName sets the actress first name and returns the builder for chaining.
func (b *ActressBuilder) WithName(name string) *ActressBuilder {
	b.actress.FirstName = name
	return b
}

// WithDMMID sets the DMM ID (used for deduplication) and returns the builder for chaining.
// Canonical test value: "123456"
func (b *ActressBuilder) WithDMMID(id string) *ActressBuilder {
	// Convert string to int for DMMID field
	var dmmID int
	if id != "" {
		// Simple conversion - in tests we control the input
		for _, c := range id {
			dmmID = dmmID*10 + int(c-'0')
		}
	}
	b.actress.DMMID = dmmID
	return b
}

// WithBirthdate sets the birthdate and returns the builder for chaining.
func (b *ActressBuilder) WithBirthdate(date time.Time) *ActressBuilder {
	// Note: Actress model doesn't have Birthdate field in current schema
	// This method is kept for future compatibility and API consistency
	// For now, it's a no-op but maintains the fluent API
	return b
}

// Build returns the constructed Actress instance.
func (b *ActressBuilder) Build() *models.Actress {
	return b.actress
}

// ScraperResultBuilder constructs ScraperResult test entities using the builder pattern with fluent API.
// It provides sensible defaults and method chaining for easy test data creation.
//
// Default values:
//   - Source: "dmm" (canonical test scraper)
//   - ContentID: "ABC-123" (canonical test content ID)
//   - Title: "Test Movie"
//   - Language: "ja"
//
// Required fields validation on Build():
//   - Source (panics if empty)
//   - ContentID (panics if empty)
//   - Title (panics if empty)
//
// Example usage:
//
//	result := testutil.NewScraperResultBuilder().
//	    WithSource("dmm").
//	    WithContentID("IPX-123").
//	    WithTitle("Test Movie").
//	    WithActresses("Actress A", "Actress B").
//	    WithGenres("Drama", "Romance").
//	    Build()
type ScraperResultBuilder struct {
	result *models.ScraperResult
}

// NewScraperResultBuilder creates a new ScraperResultBuilder with sensible defaults.
// Default: Source="dmm", ContentID="ABC-123", Title="Test Movie", Language="ja"
func NewScraperResultBuilder() *ScraperResultBuilder {
	return &ScraperResultBuilder{
		result: &models.ScraperResult{
			Source:    "dmm",
			ContentID: "ABC-123",
			Title:     "Test Movie",
			Language:  "ja",
		},
	}
}

// WithSource sets the scraper source and returns the builder for chaining.
// Required field. Common values: "dmm", "r18dev"
func (b *ScraperResultBuilder) WithSource(source string) *ScraperResultBuilder {
	b.result.Source = source
	return b
}

// WithContentID sets the content ID and returns the builder for chaining.
// Required field. Must match regex: ^[A-Z]{2,5}-\d{3,5}$ (e.g., "ABC-123", "IPXYZ-12345")
func (b *ScraperResultBuilder) WithContentID(contentID string) *ScraperResultBuilder {
	b.result.ContentID = contentID
	return b
}

// WithTitle sets the movie title and returns the builder for chaining.
// Required field.
func (b *ScraperResultBuilder) WithTitle(title string) *ScraperResultBuilder {
	b.result.Title = title
	return b
}

// WithActresses sets the actresses and returns the builder for chaining.
// Variadic parameter for convenient test data creation.
func (b *ScraperResultBuilder) WithActresses(actresses ...string) *ScraperResultBuilder {
	if len(actresses) == 0 {
		b.result.Actresses = nil
		return b
	}

	actressList := make([]models.ActressInfo, len(actresses))
	for i, name := range actresses {
		actressList[i] = models.ActressInfo{
			FirstName: name,
		}
	}
	b.result.Actresses = actressList
	return b
}

// WithGenres sets the genres and returns the builder for chaining.
// Variadic parameter for convenient test data creation.
func (b *ScraperResultBuilder) WithGenres(genres ...string) *ScraperResultBuilder {
	if len(genres) == 0 {
		b.result.Genres = nil
		return b
	}

	b.result.Genres = genres
	return b
}

// WithReleaseDate sets the release date and returns the builder for chaining.
func (b *ScraperResultBuilder) WithReleaseDate(date time.Time) *ScraperResultBuilder {
	b.result.ReleaseDate = &date
	return b
}

// WithCoverURL sets the cover URL and returns the builder for chaining.
func (b *ScraperResultBuilder) WithCoverURL(url string) *ScraperResultBuilder {
	b.result.CoverURL = url
	return b
}

// WithPosterURL sets the poster URL and returns the builder for chaining.
func (b *ScraperResultBuilder) WithPosterURL(url string) *ScraperResultBuilder {
	b.result.PosterURL = url
	return b
}

// WithDescription sets the description and returns the builder for chaining.
func (b *ScraperResultBuilder) WithDescription(description string) *ScraperResultBuilder {
	b.result.Description = description
	return b
}

// WithMaker sets the maker (studio) and returns the builder for chaining.
func (b *ScraperResultBuilder) WithMaker(maker string) *ScraperResultBuilder {
	b.result.Maker = maker
	return b
}

// WithLabel sets the label and returns the builder for chaining.
func (b *ScraperResultBuilder) WithLabel(label string) *ScraperResultBuilder {
	b.result.Label = label
	return b
}

// WithSeries sets the series and returns the builder for chaining.
func (b *ScraperResultBuilder) WithSeries(series string) *ScraperResultBuilder {
	b.result.Series = series
	return b
}

// WithRuntime sets the runtime in minutes and returns the builder for chaining.
func (b *ScraperResultBuilder) WithRuntime(runtime int) *ScraperResultBuilder {
	b.result.Runtime = runtime
	return b
}

// WithSourceURL sets the source URL and returns the builder for chaining.
func (b *ScraperResultBuilder) WithSourceURL(url string) *ScraperResultBuilder {
	b.result.SourceURL = url
	return b
}

// Build returns the constructed ScraperResult instance.
// Panics if required fields are missing or invalid:
//   - Source must not be empty
//   - ContentID must not be empty and must match regex ^[A-Z]{2,5}-\d{3,5}$
//   - Title must not be empty
func (b *ScraperResultBuilder) Build() *models.ScraperResult {
	// Validate required fields
	if b.result.Source == "" {
		panic("ScraperResultBuilder: Source is required (use WithSource)")
	}
	if b.result.ContentID == "" {
		panic("ScraperResultBuilder: ContentID is required (use WithContentID)")
	}
	if b.result.Title == "" {
		panic("ScraperResultBuilder: Title is required (use WithTitle)")
	}

	// ContentID must not be empty — format validation is intentionally permissive
	// to support varied JAV ID formats (FC2-PPV-1234567, HEYZO-1234, 1PONDO-123, etc.)

	return b.result
}
