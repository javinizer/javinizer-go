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
	b.movie.CoverURL = url
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
