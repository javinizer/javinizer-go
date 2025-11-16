package testutil

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestMovieBuilderDefaults verifies that NewMovieBuilder returns a Movie with sensible defaults.
func TestMovieBuilderDefaults(t *testing.T) {
	movie := NewMovieBuilder().Build()

	assert.NotNil(t, movie, "Movie should not be nil")
	assert.Equal(t, "IPX-123", movie.ID, "Default ID should be IPX-123")
	assert.Equal(t, "ipx00123", movie.ContentID, "Default ContentID should be ipx00123")
	assert.Equal(t, "Test Movie", movie.Title, "Default Title should be Test Movie")
}

// TestMovieBuilderWithTitle tests the WithTitle method.
func TestMovieBuilderWithTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "basic title",
			title: "Custom Title",
			want:  "Custom Title",
		},
		{
			name:  "empty title",
			title: "",
			want:  "",
		},
		{
			name:  "unicode title",
			title: "日本語タイトル",
			want:  "日本語タイトル",
		},
		{
			name:  "very long title",
			title: strings.Repeat("A", 10000),
			want:  strings.Repeat("A", 10000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := NewMovieBuilder().
				WithTitle(tt.title).
				Build()

			assert.Equal(t, tt.want, movie.Title)
		})
	}
}

// TestMovieBuilderWithActresses tests the WithActresses method.
func TestMovieBuilderWithActresses(t *testing.T) {
	tests := []struct {
		name      string
		actresses []string
		wantCount int
		wantNil   bool
	}{
		{
			name:      "single actress",
			actresses: []string{"Actress 1"},
			wantCount: 1,
			wantNil:   false,
		},
		{
			name:      "multiple actresses",
			actresses: []string{"Actress 1", "Actress 2", "Actress 3"},
			wantCount: 3,
			wantNil:   false,
		},
		{
			name:      "empty array",
			actresses: []string{},
			wantCount: 0,
			wantNil:   false,
		},
		{
			name:      "nil array",
			actresses: nil,
			wantCount: 0,
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := NewMovieBuilder().
				WithActresses(tt.actresses).
				Build()

			if tt.wantNil {
				assert.Nil(t, movie.Actresses)
			} else {
				assert.NotNil(t, movie.Actresses)
				assert.Equal(t, tt.wantCount, len(movie.Actresses))

				// Verify actress names if not empty
				if tt.wantCount > 0 {
					for i, name := range tt.actresses {
						assert.Equal(t, name, movie.Actresses[i].FirstName)
					}
				}
			}
		})
	}
}

// TestMovieBuilderWithGenres tests the WithGenres method.
func TestMovieBuilderWithGenres(t *testing.T) {
	tests := []struct {
		name      string
		genres    []string
		wantCount int
		wantNil   bool
	}{
		{
			name:      "single genre",
			genres:    []string{"Drama"},
			wantCount: 1,
			wantNil:   false,
		},
		{
			name:      "multiple genres",
			genres:    []string{"Drama", "Comedy", "Action"},
			wantCount: 3,
			wantNil:   false,
		},
		{
			name:      "empty array",
			genres:    []string{},
			wantCount: 0,
			wantNil:   false,
		},
		{
			name:      "nil array",
			genres:    nil,
			wantCount: 0,
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := NewMovieBuilder().
				WithGenres(tt.genres).
				Build()

			if tt.wantNil {
				assert.Nil(t, movie.Genres)
			} else {
				assert.NotNil(t, movie.Genres)
				assert.Equal(t, tt.wantCount, len(movie.Genres))

				// Verify genre names if not empty
				if tt.wantCount > 0 {
					for i, name := range tt.genres {
						assert.Equal(t, name, movie.Genres[i].Name)
					}
				}
			}
		})
	}
}

// TestMovieBuilderWithReleaseDate tests the WithReleaseDate method.
func TestMovieBuilderWithReleaseDate(t *testing.T) {
	testDate := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	movie := NewMovieBuilder().
		WithReleaseDate(testDate).
		Build()

	assert.NotNil(t, movie.ReleaseDate)
	assert.Equal(t, testDate, *movie.ReleaseDate)
}

// TestMovieBuilderWithCoverURL tests the WithCoverURL method.
func TestMovieBuilderWithCoverURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "basic URL",
			url:  "https://example.com/cover.jpg",
			want: "https://example.com/cover.jpg",
		},
		{
			name: "empty URL",
			url:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := NewMovieBuilder().
				WithCoverURL(tt.url).
				Build()

			assert.Equal(t, tt.want, movie.CoverURL)
		})
	}
}

// TestMovieBuilderMethodChaining tests fluent API method chaining.
func TestMovieBuilderMethodChaining(t *testing.T) {
	testDate := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	// Build a complex movie using method chaining
	movie := NewMovieBuilder().
		WithTitle("Chained Title").
		WithActresses([]string{"Actress A", "Actress B"}).
		WithGenres([]string{"Genre X"}).
		WithReleaseDate(testDate).
		WithCoverURL("https://example.com/cover.jpg").
		WithDescription("Chained description").
		WithStudio("Test Studio").
		Build()

	// Verify all fields were set correctly
	assert.Equal(t, "Chained Title", movie.Title)
	assert.Equal(t, 2, len(movie.Actresses))
	assert.Equal(t, "Actress A", movie.Actresses[0].FirstName)
	assert.Equal(t, "Actress B", movie.Actresses[1].FirstName)
	assert.Equal(t, 1, len(movie.Genres))
	assert.Equal(t, "Genre X", movie.Genres[0].Name)
	assert.NotNil(t, movie.ReleaseDate)
	assert.Equal(t, testDate, *movie.ReleaseDate)
	assert.Equal(t, "https://example.com/cover.jpg", movie.CoverURL)
	assert.Equal(t, "Chained description", movie.Description)
	assert.Equal(t, "Test Studio", movie.Maker)
}

// TestMovieBuilderWithDescription tests the WithDescription method.
func TestMovieBuilderWithDescription(t *testing.T) {
	description := "This is a test movie description"

	movie := NewMovieBuilder().
		WithDescription(description).
		Build()

	assert.Equal(t, description, movie.Description)
}

// TestMovieBuilderWithStudio tests the WithStudio method.
func TestMovieBuilderWithStudio(t *testing.T) {
	studio := "Test Studio Productions"

	movie := NewMovieBuilder().
		WithStudio(studio).
		Build()

	assert.Equal(t, studio, movie.Maker)
}

// TestMovieBuilderCanonicalID verifies the canonical test ID convention.
func TestMovieBuilderCanonicalID(t *testing.T) {
	movie := NewMovieBuilder().Build()

	// Canonical test ID should be "IPX-123" per Architecture Decision 3
	assert.Equal(t, "IPX-123", movie.ID, "Canonical test ID should be IPX-123")
}

// TestMovieBuilderWithID tests the WithID method.
func TestMovieBuilderWithID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{
			name: "custom ID",
			id:   "ABC-456",
			want: "ABC-456",
		},
		{
			name: "empty ID",
			id:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := NewMovieBuilder().
				WithID(tt.id).
				Build()

			assert.Equal(t, tt.want, movie.ID)
		})
	}
}

// TestMovieBuilderWithContentID tests the WithContentID method.
func TestMovieBuilderWithContentID(t *testing.T) {
	tests := []struct {
		name      string
		contentID string
		want      string
	}{
		{
			name:      "custom content ID",
			contentID: "abc00456",
			want:      "abc00456",
		},
		{
			name:      "empty content ID",
			contentID: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			movie := NewMovieBuilder().
				WithContentID(tt.contentID).
				Build()

			assert.Equal(t, tt.want, movie.ContentID)
		})
	}
}

// TestActressBuilderDefaults verifies that NewActressBuilder returns an Actress with sensible defaults.
func TestActressBuilderDefaults(t *testing.T) {
	actress := NewActressBuilder().Build()

	assert.NotNil(t, actress, "Actress should not be nil")
	assert.Equal(t, "Test Actress", actress.FirstName, "Default FirstName should be Test Actress")
	assert.Equal(t, 0, actress.DMMID, "Default DMMID should be 0")
}

// TestActressBuilderWithName tests the WithName method.
func TestActressBuilderWithName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "basic name",
			input: "Jane Doe",
			want:  "Jane Doe",
		},
		{
			name:  "empty name",
			input: "",
			want:  "",
		},
		{
			name:  "unicode name",
			input: "山田太郎",
			want:  "山田太郎",
		},
		{
			name:  "very long name",
			input: strings.Repeat("Name", 1000),
			want:  strings.Repeat("Name", 1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actress := NewActressBuilder().
				WithName(tt.input).
				Build()

			assert.Equal(t, tt.want, actress.FirstName)
		})
	}
}

// TestActressBuilderWithDMMID tests the WithDMMID method.
func TestActressBuilderWithDMMID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want int
	}{
		{
			name: "canonical test ID",
			id:   "123456",
			want: 123456,
		},
		{
			name: "different ID",
			id:   "789",
			want: 789,
		},
		{
			name: "empty ID",
			id:   "",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actress := NewActressBuilder().
				WithDMMID(tt.id).
				Build()

			assert.Equal(t, tt.want, actress.DMMID)
		})
	}
}

// TestActressBuilderWithBirthdate tests the WithBirthdate method.
// Note: Current Actress model doesn't have Birthdate field, but method exists for API consistency.
func TestActressBuilderWithBirthdate(t *testing.T) {
	testDate := time.Date(1990, 5, 15, 0, 0, 0, 0, time.UTC)

	actress := NewActressBuilder().
		WithBirthdate(testDate).
		Build()

	// Method should not panic and should return valid actress
	assert.NotNil(t, actress)
}

// TestActressBuilderMethodChaining tests fluent API method chaining.
func TestActressBuilderMethodChaining(t *testing.T) {
	testDate := time.Date(1990, 5, 15, 0, 0, 0, 0, time.UTC)

	// Build a complex actress using method chaining
	actress := NewActressBuilder().
		WithName("Chained Name").
		WithDMMID("123456").
		WithBirthdate(testDate).
		Build()

	// Verify all fields were set correctly
	assert.Equal(t, "Chained Name", actress.FirstName)
	assert.Equal(t, 123456, actress.DMMID)
}

// TestActressBuilderCanonicalDMMID verifies the canonical test DMMID convention.
func TestActressBuilderCanonicalDMMID(t *testing.T) {
	actress := NewActressBuilder().
		WithDMMID("123456").
		Build()

	// Canonical test DMMID should be "123456" per Architecture Decision 3
	assert.Equal(t, 123456, actress.DMMID, "Canonical test DMMID should be 123456")
}

// Example test demonstrating usage patterns (documentation via example).
func ExampleMovieBuilder() {
	// Create a movie with all default values
	defaultMovie := NewMovieBuilder().Build()
	_ = defaultMovie // defaultMovie.ID == "IPX-123", defaultMovie.Title == "Test Movie"

	// Create a custom movie with method chaining
	customMovie := NewMovieBuilder().
		WithTitle("My Custom Movie").
		WithActresses([]string{"Actress 1", "Actress 2"}).
		WithGenres([]string{"Drama", "Romance"}).
		Build()
	_ = customMovie

	// Output shows the builder pattern in action
}

// Example test demonstrating actress builder usage patterns.
func ExampleActressBuilder() {
	// Create an actress with default values
	defaultActress := NewActressBuilder().Build()
	_ = defaultActress // defaultActress.FirstName == "Test Actress"

	// Create a custom actress with canonical test DMMID
	customActress := NewActressBuilder().
		WithName("Jane Doe").
		WithDMMID("123456").
		Build()
	_ = customActress

	// Output shows the builder pattern in action
}
