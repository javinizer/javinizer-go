package models

// All tests in this package are safe for parallel execution (no shared state).
// Pure validation logic with no database writes or global config modifications.
// Reference: Architecture Decision 8 (concurrent testing with -race flag)

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Embedded golden files for JSON marshaling tests
//
//go:embed testdata/movie_full.json.golden
var movieFullGolden []byte

//go:embed testdata/movie_minimal.json.golden
var movieMinimalGolden []byte

// TestMovieCreation tests Movie struct creation with various field configurations (AC-2.3.1)
func TestMovieCreation(t *testing.T) {
	tests := []struct {
		name    string
		builder func() *Movie
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid movie with all fields",
			builder: func() *Movie {
				releaseDate := time.Date(2023, 5, 15, 0, 0, 0, 0, time.UTC)
				return &Movie{
					ContentID:   "ipx00123",
					ID:          "IPX-123",
					Title:       "Test Movie Full",
					Description: "A comprehensive test movie with all fields populated",
					ReleaseDate: &releaseDate,
					CoverURL:    "https://example.com/cover.jpg",
					Maker:       "Test Studio",
					Actresses: []Actress{
						{FirstName: "Test Actress 1"},
						{FirstName: "Test Actress 2"},
					},
					Genres: []Genre{
						{Name: "Drama"},
						{Name: "Romance"},
					},
				}
			},
			wantErr: false,
		},
		{
			name: "valid movie with minimal fields",
			builder: func() *Movie {
				return &Movie{
					ContentID: "ipx00123",
					Title:     "Test Movie Minimal",
				}
			},
			wantErr: false,
		},
		{
			name: "invalid movie with empty content id",
			builder: func() *Movie {
				return &Movie{
					ContentID: "",
					Title:     "Test Movie",
				}
			},
			wantErr: true,
			errMsg:  "ContentID cannot be empty",
		},
		{
			name: "invalid movie with empty title",
			builder: func() *Movie {
				return &Movie{
					ContentID: "ipx00123",
					Title:     "",
				}
			},
			wantErr: true,
			errMsg:  "Title cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			movie := tt.builder()

			// Validate movie
			err := validateMovie(movie)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, movie)
			assert.NotEmpty(t, movie.ContentID, "ContentID should not be empty")
			assert.NotEmpty(t, movie.Title, "Title should not be empty")
		})
	}
}

// validateMovie validates basic Movie struct requirements
func validateMovie(m *Movie) error {
	if m.ContentID == "" {
		return &ValidationError{Field: "ContentID", Message: "ContentID cannot be empty"}
	}
	if m.Title == "" {
		return &ValidationError{Field: "Title", Message: "Title cannot be empty"}
	}
	return nil
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// TestContentIDValidation tests ContentID format validation (AC-2.3.2)
func TestContentIDValidation(t *testing.T) {
	tests := []struct {
		name      string
		contentID string
		want      bool
		errMsg    string
	}{
		{
			name:      "valid standard format IPX-123",
			contentID: "IPX-123",
			want:      true,
		},
		{
			name:      "valid standard format ABC-456",
			contentID: "ABC-456",
			want:      true,
		},
		{
			name:      "valid standard format STARS-789",
			contentID: "STARS-789",
			want:      true,
		},
		{
			name:      "valid h_prefix format for DMM",
			contentID: "h_1234abc567",
			want:      true,
		},
		{
			name:      "invalid missing hyphen",
			contentID: "IPX123",
			want:      false,
			errMsg:    "invalid format",
		},
		{
			name:      "invalid too short",
			contentID: "A-1",
			want:      false,
			errMsg:    "too short",
		},
		{
			name:      "invalid special characters",
			contentID: "IPX-12#",
			want:      false,
			errMsg:    "invalid characters",
		},
		{
			name:      "invalid empty string",
			contentID: "",
			want:      false,
			errMsg:    "cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateContentID(tt.contentID)

			if tt.want {
				assert.NoError(t, err, "ContentID %s should be valid", tt.contentID)
			} else {
				assert.Error(t, err, "ContentID %s should be invalid", tt.contentID)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// validateContentID validates ContentID format
func validateContentID(contentID string) error {
	if contentID == "" {
		return &ValidationError{Field: "ContentID", Message: "ContentID cannot be empty"}
	}

	// Check length (minimum: "A-1" = 3 chars, but realistically should be longer)
	if len(contentID) < 5 {
		return &ValidationError{Field: "ContentID", Message: "ContentID too short"}
	}

	// Check for h_prefix format (DMM format)
	if len(contentID) > 2 && contentID[0] == 'h' && contentID[1] == '_' {
		// h_prefix format is valid
		return nil
	}

	// Check for standard format: LETTERS-NUMBERS
	hasHyphen := false
	hasInvalidChars := false
	for _, c := range contentID {
		if c == '-' {
			hasHyphen = true
			continue
		}
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			hasInvalidChars = true
			break
		}
	}

	if !hasHyphen {
		return &ValidationError{Field: "ContentID", Message: "ContentID invalid format: missing hyphen"}
	}

	if hasInvalidChars {
		return &ValidationError{Field: "ContentID", Message: "ContentID invalid characters"}
	}

	return nil
}

// TestMovieJSONMarshaling tests JSON serialization and deserialization (AC-2.3.3)
func TestMovieJSONMarshaling(t *testing.T) {
	t.Run("marshal full movie to JSON", func(t *testing.T) {
		t.Parallel()
		// Create a movie with all fields
		releaseDate := time.Date(2023, 5, 15, 0, 0, 0, 0, time.UTC)
		createdAt := time.Date(2023, 1, 15, 10, 30, 0, 0, time.UTC)
		updatedAt := time.Date(2023, 1, 15, 10, 30, 0, 0, time.UTC)

		movie := &Movie{
			ContentID:        "ipx00123",
			ID:               "IPX-123",
			Title:            "Test Movie Full",
			Description:      "A comprehensive test movie with all fields populated for testing JSON marshaling",
			ReleaseDate:      &releaseDate,
			ReleaseYear:      2023,
			Runtime:          120,
			Director:         "Test Director",
			Maker:            "Test Studio",
			Label:            "Test Label",
			Series:           "Test Series",
			RatingScore:      8.5,
			RatingVotes:      100,
			PosterURL:        "https://example.com/poster.jpg",
			CoverURL:         "https://example.com/cover.jpg",
			TrailerURL:       "https://example.com/trailer.mp4",
			OriginalFileName: "IPX-123.mp4",
			Actresses: []Actress{
				{
					DMMID:        123456,
					FirstName:    "Test",
					LastName:     "Actress",
					JapaneseName: "テスト女優",
					ThumbURL:     "https://example.com/actress.jpg",
				},
			},
			Genres: []Genre{
				{Name: "Drama"},
				{Name: "Romance"},
			},
			Screenshots: []string{
				"https://example.com/screenshot1.jpg",
				"https://example.com/screenshot2.jpg",
				"https://example.com/screenshot3.jpg",
			},
			SourceName: "r18dev",
			SourceURL:  "https://www.r18.com/videos/vod/movies/detail/-/id=ipx00123/",
			CreatedAt:  createdAt,
			UpdatedAt:  updatedAt,
		}

		// Marshal to JSON
		actualJSON, err := json.MarshalIndent(movie, "", "  ")
		require.NoError(t, err, "Failed to marshal movie to JSON")

		// Compare with golden file
		var expected, actual map[string]interface{}
		err = json.Unmarshal(movieFullGolden, &expected)
		require.NoError(t, err, "Failed to unmarshal golden file")
		err = json.Unmarshal(actualJSON, &actual)
		require.NoError(t, err, "Failed to unmarshal actual JSON")

		// Compare key fields (not exact match due to time formatting differences)
		assert.Equal(t, expected["content_id"], actual["content_id"])
		assert.Equal(t, expected["id"], actual["id"])
		assert.Equal(t, expected["title"], actual["title"])
		assert.Equal(t, expected["description"], actual["description"])
		assert.Equal(t, expected["release_year"], actual["release_year"])
		assert.Equal(t, expected["runtime"], actual["runtime"])
		assert.NotNil(t, actual["actresses"])
		assert.NotNil(t, actual["genres"])
		assert.NotNil(t, actual["screenshot_urls"])
	})

	t.Run("marshal minimal movie to JSON", func(t *testing.T) {
		t.Parallel()
		movie := &Movie{
			ContentID: "ipx00123",
			Title:     "Test Movie Minimal",
		}

		actualJSON, err := json.MarshalIndent(movie, "", "  ")
		require.NoError(t, err, "Failed to marshal minimal movie to JSON")

		var expected, actual map[string]interface{}
		err = json.Unmarshal(movieMinimalGolden, &expected)
		require.NoError(t, err, "Failed to unmarshal golden file")
		err = json.Unmarshal(actualJSON, &actual)
		require.NoError(t, err, "Failed to unmarshal actual JSON")

		// Compare key fields
		assert.Equal(t, expected["content_id"], actual["content_id"])
		assert.Equal(t, expected["title"], actual["title"])
	})

	t.Run("unmarshal valid JSON to Movie struct", func(t *testing.T) {
		t.Parallel()
		var movie Movie
		err := json.Unmarshal(movieFullGolden, &movie)

		require.NoError(t, err, "Failed to unmarshal JSON to Movie")
		assert.Equal(t, "ipx00123", movie.ContentID)
		assert.Equal(t, "IPX-123", movie.ID)
		assert.Equal(t, "Test Movie Full", movie.Title)
		assert.NotNil(t, movie.ReleaseDate)
		assert.Equal(t, 2023, movie.ReleaseYear)
		assert.NotEmpty(t, movie.Actresses)
		assert.NotEmpty(t, movie.Genres)
		assert.NotEmpty(t, movie.Screenshots)
	})

	t.Run("unmarshal invalid JSON returns error", func(t *testing.T) {
		t.Parallel()
		invalidJSON := []byte(`{"content_id": 123, "title": "Test"}`)
		var movie Movie
		err := json.Unmarshal(invalidJSON, &movie)

		// JSON unmarshaling returns error for type mismatches
		// This test ensures we handle JSON errors gracefully
		assert.Error(t, err, "Should return error for type mismatch")
		assert.Contains(t, err.Error(), "cannot unmarshal number")
	})

	t.Run("nil ReleaseDate pointer handles correctly", func(t *testing.T) {
		t.Parallel()
		movie := &Movie{
			ContentID:   "ipx00123",
			Title:       "Test Movie",
			ReleaseDate: nil,
		}

		jsonData, err := json.Marshal(movie)
		require.NoError(t, err, "Failed to marshal movie with nil ReleaseDate")

		var unmarshaled Movie
		err = json.Unmarshal(jsonData, &unmarshaled)
		require.NoError(t, err, "Failed to unmarshal movie with nil ReleaseDate")
		assert.Nil(t, unmarshaled.ReleaseDate)
	})

	t.Run("Screenshots array serializes to JSON array", func(t *testing.T) {
		t.Parallel()
		movie := &Movie{
			ContentID: "ipx00123",
			Title:     "Test Movie",
			Screenshots: []string{
				"https://example.com/screenshot1.jpg",
				"https://example.com/screenshot2.jpg",
			},
		}

		jsonData, err := json.Marshal(movie)
		require.NoError(t, err)

		var result map[string]interface{}
		err = json.Unmarshal(jsonData, &result)
		require.NoError(t, err)

		screenshots, ok := result["screenshot_urls"].([]interface{})
		assert.True(t, ok, "screenshot_urls should be an array")
		assert.Len(t, screenshots, 2)
	})

	t.Run("Actresses and Genres relationships serialize as arrays", func(t *testing.T) {
		t.Parallel()
		movie := &Movie{
			ContentID: "ipx00123",
			Title:     "Test Movie",
			Actresses: []Actress{
				{FirstName: "Actress 1"},
				{FirstName: "Actress 2"},
			},
			Genres: []Genre{
				{Name: "Drama"},
				{Name: "Action"},
			},
		}

		jsonData, err := json.Marshal(movie)
		require.NoError(t, err)

		var result map[string]interface{}
		err = json.Unmarshal(jsonData, &result)
		require.NoError(t, err)

		actresses, ok := result["actresses"].([]interface{})
		assert.True(t, ok, "actresses should be an array")
		assert.Len(t, actresses, 2)

		genres, ok := result["genres"].([]interface{})
		assert.True(t, ok, "genres should be an array")
		assert.Len(t, genres, 2)
	})
}

// TestGORMTags tests GORM struct tag validation using reflection (AC-2.3.4)
func TestGORMTags(t *testing.T) {
	t.Run("ContentID has primaryKey tag", func(t *testing.T) {
		t.Parallel()
		movieType := reflect.TypeOf(Movie{})
		field, found := movieType.FieldByName("ContentID")
		require.True(t, found, "ContentID field should exist")

		gormTag := field.Tag.Get("gorm")
		assert.Contains(t, gormTag, "primaryKey", "ContentID should have primaryKey tag")
	})

	t.Run("ID has index tag", func(t *testing.T) {
		t.Parallel()
		movieType := reflect.TypeOf(Movie{})
		field, found := movieType.FieldByName("ID")
		require.True(t, found, "ID field should exist")

		gormTag := field.Tag.Get("gorm")
		assert.Contains(t, gormTag, "index", "ID should have index tag")
	})

	t.Run("Actresses has many2many tag", func(t *testing.T) {
		t.Parallel()
		movieType := reflect.TypeOf(Movie{})
		field, found := movieType.FieldByName("Actresses")
		require.True(t, found, "Actresses field should exist")

		gormTag := field.Tag.Get("gorm")
		assert.Contains(t, gormTag, "many2many", "Actresses should have many2many tag")
		assert.Contains(t, gormTag, "movie_actresses", "Actresses should reference movie_actresses join table")
	})

	t.Run("Genres has many2many tag", func(t *testing.T) {
		t.Parallel()
		movieType := reflect.TypeOf(Movie{})
		field, found := movieType.FieldByName("Genres")
		require.True(t, found, "Genres field should exist")

		gormTag := field.Tag.Get("gorm")
		assert.Contains(t, gormTag, "many2many", "Genres should have many2many tag")
		assert.Contains(t, gormTag, "movie_genres", "Genres should reference movie_genres join table")
	})

	t.Run("Translations has foreignKey tag", func(t *testing.T) {
		t.Parallel()
		movieType := reflect.TypeOf(Movie{})
		field, found := movieType.FieldByName("Translations")
		require.True(t, found, "Translations field should exist")

		gormTag := field.Tag.Get("gorm")
		assert.Contains(t, gormTag, "foreignKey", "Translations should have foreignKey tag")
		assert.Contains(t, gormTag, "MovieID", "Translations should reference MovieID foreign key")
	})

	t.Run("Screenshots has serializer:json tag", func(t *testing.T) {
		t.Parallel()
		movieType := reflect.TypeOf(Movie{})
		field, found := movieType.FieldByName("Screenshots")
		require.True(t, found, "Screenshots field should exist")

		gormTag := field.Tag.Get("gorm")
		assert.Contains(t, gormTag, "serializer:json", "Screenshots should have serializer:json tag")
	})
}

// TestFieldValidation tests business logic validation for Movie fields (AC-2.3.5)
func TestFieldValidation(t *testing.T) {
	t.Run("ReleaseDate in future", func(t *testing.T) {
		t.Parallel()
		futureDate := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC) // Fixed future date
		movie := &Movie{
			ContentID:   "ipx00123",
			Title:       "Test Movie",
			ReleaseDate: &futureDate,
		}

		err := validateMovieFields(movie)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "future")
	})

	t.Run("ReleaseDate before 1900", func(t *testing.T) {
		t.Parallel()
		oldDate := time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
		movie := &Movie{
			ContentID:   "ipx00123",
			Title:       "Test Movie",
			ReleaseDate: &oldDate,
		}

		err := validateMovieFields(movie)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "1900")
	})

	t.Run("Runtime negative value", func(t *testing.T) {
		t.Parallel()
		movie := &Movie{
			ContentID: "ipx00123",
			Title:     "Test Movie",
			Runtime:   -10,
		}

		err := validateMovieFields(movie)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Runtime")
	})

	t.Run("RatingScore out of range high", func(t *testing.T) {
		t.Parallel()
		movie := &Movie{
			ContentID:   "ipx00123",
			Title:       "Test Movie",
			RatingScore: 11.0,
		}

		err := validateMovieFields(movie)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "RatingScore")
	})

	t.Run("RatingScore out of range low", func(t *testing.T) {
		t.Parallel()
		movie := &Movie{
			ContentID:   "ipx00123",
			Title:       "Test Movie",
			RatingScore: -1.0,
		}

		err := validateMovieFields(movie)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "RatingScore")
	})

	t.Run("RatingVotes negative", func(t *testing.T) {
		t.Parallel()
		movie := &Movie{
			ContentID:   "ipx00123",
			Title:       "Test Movie",
			RatingVotes: -5,
		}

		err := validateMovieFields(movie)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "RatingVotes")
	})

	t.Run("Empty arrays are valid", func(t *testing.T) {
		t.Parallel()
		movie := &Movie{
			ContentID:   "ipx00123",
			Title:       "Test Movie",
			Actresses:   []Actress{},
			Genres:      []Genre{},
			Screenshots: []string{},
		}

		err := validateMovieFields(movie)
		assert.NoError(t, err)
	})

	t.Run("Very long Description is valid", func(t *testing.T) {
		t.Parallel()
		longDesc := make([]byte, 10000)
		for i := range longDesc {
			longDesc[i] = 'a'
		}

		movie := &Movie{
			ContentID:   "ipx00123",
			Title:       "Test Movie",
			Description: string(longDesc),
		}

		err := validateMovieFields(movie)
		assert.NoError(t, err)
	})

	t.Run("Unicode in Title is valid", func(t *testing.T) {
		t.Parallel()
		movie := &Movie{
			ContentID: "ipx00123",
			Title:     "テスト映画 Test Movie 测试电影",
		}

		err := validateMovieFields(movie)
		assert.NoError(t, err)
	})
}

// validateMovieFields validates business logic rules for Movie fields
func validateMovieFields(m *Movie) error {
	// Validate ReleaseDate
	if m.ReleaseDate != nil {
		if m.ReleaseDate.After(time.Now()) {
			return &ValidationError{Field: "ReleaseDate", Message: "ReleaseDate cannot be in the future"}
		}
		if m.ReleaseDate.Year() < 1900 {
			return &ValidationError{Field: "ReleaseDate", Message: "ReleaseDate cannot be before 1900"}
		}
	}

	// Validate Runtime
	if m.Runtime < 0 {
		return &ValidationError{Field: "Runtime", Message: "Runtime cannot be negative"}
	}

	// Validate RatingScore
	if m.RatingScore < 0 || m.RatingScore > 10 {
		return &ValidationError{Field: "RatingScore", Message: "RatingScore must be between 0 and 10"}
	}

	// Validate RatingVotes
	if m.RatingVotes < 0 {
		return &ValidationError{Field: "RatingVotes", Message: "RatingVotes cannot be negative"}
	}

	return nil
}
