package testutil

import (
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestmovieBuilderDefaults verifies that NewMovieBuilder returns a Movie with sensible defaults.
func TestMovieBuilderDefaults(t *testing.T) {
	movie := NewMovieBuilder().Build()

	assert.NotNil(t, movie, "Movie should not be nil")
	assert.Equal(t, "IPX-123", movie.ID, "Default ID should be IPX-123")
	assert.Equal(t, "ipx00123", movie.ContentID, "Default ContentID should be ipx00123")
	assert.Equal(t, "Test Movie", movie.Title, "Default Title should be Test Movie")
}

// TestmovieBuilderWithTitle tests the WithTitle method.
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

// TestmovieBuilderWithActresses tests the WithActresses method.
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

// TestmovieBuilderWithGenres tests the WithGenres method.
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

// TestmovieBuilderWithReleaseDate tests the WithReleaseDate method.
func TestMovieBuilderWithReleaseDate(t *testing.T) {
	testDate := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	movie := NewMovieBuilder().
		WithReleaseDate(testDate).
		Build()

	assert.NotNil(t, movie.ReleaseDate)
	assert.Equal(t, testDate, *movie.ReleaseDate)
}

// TestmovieBuilderWithCoverURL tests the WithCoverURL method.
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

			assert.Equal(t, tt.want, movie.Poster.CoverURL)
		})
	}
}

// TestmovieBuilderMethodChaining tests fluent API method chaining.
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
	assert.Equal(t, "https://example.com/cover.jpg", movie.Poster.CoverURL)
	assert.Equal(t, "Chained description", movie.Description)
	assert.Equal(t, "Test Studio", movie.Maker)
}

// TestmovieBuilderWithDescription tests the WithDescription method.
func TestMovieBuilderWithDescription(t *testing.T) {
	description := "This is a test movie description"

	movie := NewMovieBuilder().
		WithDescription(description).
		Build()

	assert.Equal(t, description, movie.Description)
}

// TestmovieBuilderWithStudio tests the WithStudio method.
func TestMovieBuilderWithStudio(t *testing.T) {
	studio := "Test Studio Productions"

	movie := NewMovieBuilder().
		WithStudio(studio).
		Build()

	assert.Equal(t, studio, movie.Maker)
}

// TestmovieBuilderCanonicalID verifies the canonical test ID convention.
func TestMovieBuilderCanonicalID(t *testing.T) {
	movie := NewMovieBuilder().Build()

	// Canonical test ID should be "IPX-123" per Architecture Decision 3
	assert.Equal(t, "IPX-123", movie.ID, "Canonical test ID should be IPX-123")
}

// TestmovieBuilderWithID tests the WithID method.
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

// TestmovieBuilderWithContentID tests the WithContentID method.
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

// TestactressBuilderDefaults verifies that NewActressBuilder returns an Actress with sensible defaults.
func TestActressBuilderDefaults(t *testing.T) {
	actress := NewActressBuilder().Build()

	assert.NotNil(t, actress, "Actress should not be nil")
	assert.Equal(t, "Test Actress", actress.FirstName, "Default FirstName should be Test Actress")
	assert.Equal(t, 0, actress.DMMID, "Default DMMID should be 0")
}

// TestactressBuilderWithName tests the WithName method.
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

// TestactressBuilderWithDMMID tests the WithDMMID method.
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

// TestactressBuilderWithBirthdate tests the WithBirthdate method.
// Note: Current Actress model doesn't have Birthdate field, but method exists for API consistency.
func TestActressBuilderWithBirthdate(t *testing.T) {
	testDate := time.Date(1990, 5, 15, 0, 0, 0, 0, time.UTC)

	actress := NewActressBuilder().
		WithBirthdate(testDate).
		Build()

	// Method should not panic and should return valid actress
	assert.NotNil(t, actress)
}

// TestactressBuilderMethodChaining tests fluent API method chaining.
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

// TestactressBuilderCanonicalDMMID verifies the canonical test DMMID convention.
func TestActressBuilderCanonicalDMMID(t *testing.T) {
	actress := NewActressBuilder().
		WithDMMID("123456").
		Build()

	// Canonical test DMMID should be "123456" per Architecture Decision 3
	assert.Equal(t, 123456, actress.DMMID, "Canonical test DMMID should be 123456")
}

// TestscraperResultBuilderDefaults verifies the default values set by NewScraperResultBuilder.
func TestScraperResultBuilderDefaults(t *testing.T) {
	result := NewScraperResultBuilder().Build()

	assert.Equal(t, "dmm", result.Source, "default source should be 'dmm'")
	assert.Equal(t, "ABC-123", result.ContentID, "default contentID should be 'ABC-123'")
	assert.Equal(t, "Test Movie", result.Title, "default title should be 'Test Movie'")
	assert.Equal(t, "ja", result.Language, "default language should be 'ja'")
	assert.Nil(t, result.Actresses, "default actresses should be nil")
	assert.Nil(t, result.Genres, "default genres should be nil")
}

// TestscraperResultBuilderRequiredFields verifies that Build() panics when required fields are missing or invalid.
func TestScraperResultBuilderRequiredFields(t *testing.T) {
	tests := []struct {
		name        string
		builderFunc func() *ScraperResultBuilder
		wantPanic   string
	}{
		{
			name: "missing source",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithSource("")
			},
			wantPanic: "Source is required",
		},
		{
			name: "missing contentID",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("")
			},
			wantPanic: "ContentID is required",
		},
		{
			name: "missing title",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithTitle("")
			},
			wantPanic: "Title is required",
		},
		{
			name: "contentID too short prefix",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("A-123")
			},
			wantPanic: "", // Permissive — no format validation
		},
		{
			name: "contentID too long prefix",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("ABCDEF-123")
			},
			wantPanic: "", // Permissive
		},
		{
			name: "contentID too short suffix",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("ABC-12")
			},
			wantPanic: "", // Permissive
		},
		{
			name: "contentID too long suffix",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("ABC-123456")
			},
			wantPanic: "", // Permissive
		},
		{
			name: "contentID missing hyphen",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("ABC123")
			},
			wantPanic: "", // Permissive — no format validation
		},
		{
			name: "valid 2-letter prefix",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("AB-123")
			},
			wantPanic: "", // Should NOT panic
		},
		{
			name: "valid 3-letter prefix",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("ABC-123")
			},
			wantPanic: "", // Should NOT panic
		},
		{
			name: "valid 5-letter prefix",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("IPXYZ-123")
			},
			wantPanic: "", // Should NOT panic
		},
		{
			name: "valid 3-digit suffix",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("ABC-123")
			},
			wantPanic: "", // Should NOT panic
		},
		{
			name: "valid 5-digit suffix",
			builderFunc: func() *ScraperResultBuilder {
				return NewScraperResultBuilder().WithContentID("ABC-12345")
			},
			wantPanic: "", // Should NOT panic
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic == "" {
				// Should NOT panic
				assert.NotPanics(t, func() {
					_ = tt.builderFunc().Build()
				})
			} else {
				// Should panic with message containing wantPanic string
				assert.Panics(t, func() {
					defer func() {
						if r := recover(); r != nil {
							panicMsg, ok := r.(string)
							assert.True(t, ok, "panic value should be a string")
							assert.Contains(t, panicMsg, tt.wantPanic, "panic message should contain expected text")
							panic(r) // Re-panic to satisfy assert.Panics
						}
					}()
					_ = tt.builderFunc().Build()
				})
			}
		})
	}
}

// TestscraperResultBuilderWithSource verifies WithSource sets the source correctly.
func TestScraperResultBuilderWithSource(t *testing.T) {
	result := NewScraperResultBuilder().
		WithSource("r18dev").
		Build()

	assert.Equal(t, "r18dev", result.Source)
}

// TestscraperResultBuilderWithContentID verifies WithContentID sets the content ID correctly.
func TestScraperResultBuilderWithContentID(t *testing.T) {
	result := NewScraperResultBuilder().
		WithContentID("IPX-123").
		Build()

	assert.Equal(t, "IPX-123", result.ContentID)
}

// TestscraperResultBuilderWithActresses verifies WithActresses handles variadic arguments correctly.
func TestScraperResultBuilderWithActresses(t *testing.T) {
	tests := []struct {
		name      string
		actresses []string
		want      []models.ActressInfo
	}{
		{
			name:      "nil actresses",
			actresses: nil,
			want:      nil,
		},
		{
			name:      "empty actresses",
			actresses: []string{},
			want:      nil,
		},
		{
			name:      "single actress",
			actresses: []string{"Actress A"},
			want: []models.ActressInfo{
				{FirstName: "Actress A"},
			},
		},
		{
			name:      "multiple actresses",
			actresses: []string{"Actress A", "Actress B", "Actress C"},
			want: []models.ActressInfo{
				{FirstName: "Actress A"},
				{FirstName: "Actress B"},
				{FirstName: "Actress C"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewScraperResultBuilder().
				WithActresses(tt.actresses...).
				Build()

			assert.Equal(t, tt.want, result.Actresses)
		})
	}
}

// TestscraperResultBuilderWithGenres verifies WithGenres handles variadic arguments correctly.
func TestScraperResultBuilderWithGenres(t *testing.T) {
	tests := []struct {
		name   string
		genres []string
		want   []string
	}{
		{
			name:   "nil genres",
			genres: nil,
			want:   nil,
		},
		{
			name:   "empty genres",
			genres: []string{},
			want:   nil,
		},
		{
			name:   "single genre",
			genres: []string{"Drama"},
			want:   []string{"Drama"},
		},
		{
			name:   "multiple genres",
			genres: []string{"Drama", "Romance", "Comedy"},
			want:   []string{"Drama", "Romance", "Comedy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewScraperResultBuilder().
				WithGenres(tt.genres...).
				Build()

			assert.Equal(t, tt.want, result.Genres)
		})
	}
}

// TestscraperResultBuilderMethodChaining verifies that all With methods support method chaining.
func TestScraperResultBuilderMethodChaining(t *testing.T) {
	releaseDate := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)

	result := NewScraperResultBuilder().
		WithSource("dmm").
		WithContentID("IPX-123").
		WithTitle("Test Movie Title").
		WithActresses("Actress A", "Actress B").
		WithGenres("Drama", "Romance").
		WithReleaseDate(releaseDate).
		WithCoverURL("https://example.com/cover.jpg").
		WithPosterURL("https://example.com/poster.jpg").
		WithDescription("Test description").
		WithMaker("Test Studio").
		WithLabel("Test Label").
		WithSeries("Test Series").
		WithRuntime(120).
		WithSourceURL("https://example.com/source").
		Build()

	assert.Equal(t, "dmm", result.Source)
	assert.Equal(t, "IPX-123", result.ContentID)
	assert.Equal(t, "Test Movie Title", result.Title)
	assert.Equal(t, 2, len(result.Actresses))
	assert.Equal(t, "Actress A", result.Actresses[0].FirstName)
	assert.Equal(t, "Actress B", result.Actresses[1].FirstName)
	assert.Equal(t, []string{"Drama", "Romance"}, result.Genres)
	assert.Equal(t, &releaseDate, result.ReleaseDate)
	assert.Equal(t, "https://example.com/cover.jpg", result.CoverURL)
	assert.Equal(t, "https://example.com/poster.jpg", result.PosterURL)
	assert.Equal(t, "Test description", result.Description)
	assert.Equal(t, "Test Studio", result.Maker)
	assert.Equal(t, "Test Label", result.Label)
	assert.Equal(t, "Test Series", result.Series)
	assert.Equal(t, 120, result.Runtime)
	assert.Equal(t, "https://example.com/source", result.SourceURL)
}

// TestscraperResultBuilderCanonicalValues verifies the builder works with canonical test values.
func TestScraperResultBuilderCanonicalValues(t *testing.T) {
	releaseDate := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)

	result := NewScraperResultBuilder().
		WithSource("dmm").
		WithContentID("IPX-123").
		WithTitle("Canonical Test Movie").
		WithActresses("Test Actress A", "Test Actress B").
		WithGenres("Drama").
		WithReleaseDate(releaseDate).
		WithCoverURL("https://pics.dmm.co.jp/digital/video/ipx123/ipx123ps.jpg").
		WithMaker("Test Studio").
		WithRuntime(120).
		Build()

	assert.Equal(t, "dmm", result.Source)
	assert.Equal(t, "IPX-123", result.ContentID)
	assert.Equal(t, "Canonical Test Movie", result.Title)
	assert.Equal(t, "ja", result.Language)
	assert.NotNil(t, result.Actresses)
	assert.Equal(t, 2, len(result.Actresses))
	assert.NotNil(t, result.Genres)
	assert.Equal(t, 1, len(result.Genres))
	assert.NotNil(t, result.ReleaseDate)
	assert.NotEmpty(t, result.CoverURL)
}
