package models

// All tests in this package are safe for parallel execution (no shared state).
// Pure validation logic with no database writes or global config modifications.
// Reference: Architecture Decision 8 (concurrent testing with -race flag)

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MovieTranslationValidationError represents a validation error for MovieTranslation
type MovieTranslationValidationError struct {
	Field   string
	Message string
}

func (e *MovieTranslationValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// validateMovieTranslation performs validation on MovieTranslation struct
func validateMovieTranslation(mt *MovieTranslation) error {
	if mt.MovieID == "" {
		return &MovieTranslationValidationError{Field: "MovieID", Message: "cannot be empty"}
	}
	if mt.Language == "" {
		return &MovieTranslationValidationError{Field: "Language", Message: "cannot be empty"}
	}
	// Validate Language is ISO 639-1 (2-letter code)
	validLanguages := map[string]bool{"en": true, "ja": true, "zh": true, "ko": true, "es": true, "fr": true, "de": true}
	if !validLanguages[mt.Language] {
		return &MovieTranslationValidationError{Field: "Language", Message: "invalid language code"}
	}
	return nil
}

// TestMovieTranslationCreation tests MovieTranslation struct creation and validation
// AC-2.6.6: Valid creation, language/entity validation, empty fields, long values
func TestMovieTranslationCreation(t *testing.T) {
	tests := []struct {
		name    string
		builder func() *MovieTranslation
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid MovieTranslation with all fields",
			builder: func() *MovieTranslation {
				return &MovieTranslation{
					ID:            1,
					MovieID:       "IPX-123",
					Language:      "en",
					Title:         "Beautiful Instructor",
					OriginalTitle: "美人インストラクター",
					Description:   "A beautiful gym instructor...",
					Director:      "John Doe",
					Maker:         "Idea Pocket",
					Label:         "IP Label",
					Series:        "IP Series",
					SourceName:    "r18dev",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with Japanese language",
			builder: func() *MovieTranslation {
				return &MovieTranslation{
					ID:            2,
					MovieID:       "IPX-456",
					Language:      "ja",
					Title:         "美人インストラクター",
					OriginalTitle: "美人インストラクター",
					Description:   "美しいジムのインストラクター...",
					Director:      "山田太郎",
					Maker:         "アイデアポケット",
					Label:         "IPレーベル",
					Series:        "IPシリーズ",
					SourceName:    "dmm",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with empty optional fields",
			builder: func() *MovieTranslation {
				return &MovieTranslation{
					ID:            3,
					MovieID:       "IPX-789",
					Language:      "en",
					Title:         "Basic Title",
					OriginalTitle: "",
					Description:   "",
					Director:      "",
					Maker:         "",
					Label:         "",
					Series:        "",
					SourceName:    "r18dev",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with very long Description (1000+ characters)",
			builder: func() *MovieTranslation {
				longDesc := strings.Repeat("A very long description text that goes on and on. ", 25) // ~1250 chars
				return &MovieTranslation{
					ID:            4,
					MovieID:       "IPX-999",
					Language:      "en",
					Title:         "Long Description Movie",
					OriginalTitle: "長い説明の映画",
					Description:   longDesc,
					Director:      "Director Name",
					Maker:         "Maker Name",
					Label:         "Label Name",
					Series:        "Series Name",
					SourceName:    "r18dev",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with Chinese language",
			builder: func() *MovieTranslation {
				return &MovieTranslation{
					ID:            5,
					MovieID:       "IPX-111",
					Language:      "zh",
					Title:         "美丽的教练",
					OriginalTitle: "美人インストラクター",
					Description:   "一位美丽的健身教练...",
					SourceName:    "r18dev",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "invalid - empty Language",
			builder: func() *MovieTranslation {
				return &MovieTranslation{
					ID:            6,
					MovieID:       "IPX-222",
					Language:      "",
					Title:         "Missing Language",
					OriginalTitle: "言語なし",
					Description:   "Missing language code",
					SourceName:    "r18dev",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "Language: cannot be empty",
		},
		{
			name: "invalid - invalid language code (xyz)",
			builder: func() *MovieTranslation {
				return &MovieTranslation{
					ID:            7,
					MovieID:       "IPX-333",
					Language:      "xyz",
					Title:         "Invalid Language",
					OriginalTitle: "無効な言語",
					Description:   "Invalid language code",
					SourceName:    "r18dev",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "Language: invalid language code",
		},
		{
			name: "invalid - empty MovieID",
			builder: func() *MovieTranslation {
				return &MovieTranslation{
					ID:            8,
					MovieID:       "",
					Language:      "en",
					Title:         "Missing MovieID",
					OriginalTitle: "MovieIDなし",
					Description:   "Missing MovieID",
					SourceName:    "r18dev",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "MovieID: cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mt := tt.builder()
			err := validateMovieTranslation(mt)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			assert.NoError(t, err)
			if !tt.wantErr {
				assert.NotEmpty(t, mt.MovieID)
				assert.NotEmpty(t, mt.Language)
			}
		})
	}
}

// TestMovieTranslationJSONMarshal tests JSON marshaling/unmarshaling
// AC-2.6.6: JSON marshaling/unmarshaling
func TestMovieTranslationJSONMarshal(t *testing.T) {
	tests := []struct {
		name string
		mt   *MovieTranslation
	}{
		{
			name: "marshal complete MovieTranslation",
			mt: &MovieTranslation{
				ID:            1,
				MovieID:       "IPX-123",
				Language:      "en",
				Title:         "Beautiful Instructor",
				OriginalTitle: "美人インストラクター",
				Description:   "A beautiful gym instructor...",
				Director:      "John Doe",
				Maker:         "Idea Pocket",
				Label:         "IP Label",
				Series:        "IP Series",
				SourceName:    "r18dev",
				CreatedAt:     time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
				UpdatedAt:     time.Date(2023, 5, 16, 14, 45, 0, 0, time.UTC),
			},
		},
		{
			name: "marshal with minimal fields",
			mt: &MovieTranslation{
				ID:            2,
				MovieID:       "IPX-456",
				Language:      "ja",
				Title:         "最小フィールド",
				OriginalTitle: "",
				Description:   "",
				SourceName:    "dmm",
				CreatedAt:     time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
				UpdatedAt:     time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Marshal to JSON
			data, err := json.Marshal(tt.mt)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to struct
			var unmarshaled MovieTranslation
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Verify fields match
			assert.Equal(t, tt.mt.ID, unmarshaled.ID)
			assert.Equal(t, tt.mt.MovieID, unmarshaled.MovieID)
			assert.Equal(t, tt.mt.Language, unmarshaled.Language)
			assert.Equal(t, tt.mt.Title, unmarshaled.Title)
			assert.Equal(t, tt.mt.OriginalTitle, unmarshaled.OriginalTitle)
		})
	}
}

// TestMovieTranslationGORMTags tests GORM tag validation via reflection
// AC-2.6.6: GORM tags validation (composite unique index)
func TestMovieTranslationGORMTags(t *testing.T) {
	mtType := reflect.TypeOf(MovieTranslation{})

	tests := []struct {
		name      string
		fieldName string
		wantTag   string
	}{
		{
			name:      "ID field has primaryKey tag",
			fieldName: "ID",
			wantTag:   "primaryKey",
		},
		{
			name:      "MovieID field has composite unique index",
			fieldName: "MovieID",
			wantTag:   "index:idx_movie_language,unique",
		},
		{
			name:      "Language field has composite unique index",
			fieldName: "Language",
			wantTag:   "index:idx_movie_language,unique",
		},
		{
			name:      "Language field has size constraint",
			fieldName: "Language",
			wantTag:   "size:5",
		},
		{
			name:      "Description field has type:text tag",
			fieldName: "Description",
			wantTag:   "type:text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			field, found := mtType.FieldByName(tt.fieldName)
			require.True(t, found, "Field %s not found", tt.fieldName)

			gormTag := field.Tag.Get("gorm")
			assert.Contains(t, gormTag, tt.wantTag, "Field %s missing expected GORM tag: %s", tt.fieldName, tt.wantTag)
		})
	}
}

// TestMovieTranslationTableName tests the TableName method
func TestMovieTranslationTableName(t *testing.T) {
	mt := MovieTranslation{}
	tableName := mt.TableName()
	assert.Equal(t, "movie_translations", tableName)
}
