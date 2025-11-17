package models

// All tests in this package are safe for parallel execution (no shared state).
// Pure validation logic with no database writes or global config modifications.
// Reference: Architecture Decision 8 (concurrent testing with -race flag)

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MovieTagValidationError represents a validation error for MovieTag
type MovieTagValidationError struct {
	Field   string
	Message string
}

func (e *MovieTagValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// validateMovieTag performs validation on MovieTag struct
func validateMovieTag(mt *MovieTag) error {
	if mt.MovieID == "" {
		return &MovieTagValidationError{Field: "MovieID", Message: "cannot be empty"}
	}
	if mt.Tag == "" {
		return &MovieTagValidationError{Field: "Tag", Message: "cannot be empty"}
	}
	return nil
}

// TestMovieTagCreation tests MovieTag struct creation and validation
// AC-2.6.3: Valid creation, zero MovieID/Tag validation
func TestMovieTagCreation(t *testing.T) {
	tests := []struct {
		name    string
		builder func() *MovieTag
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid MovieTag with all fields",
			builder: func() *MovieTag {
				return &MovieTag{
					ID:        1,
					MovieID:   "IPX-123",
					Tag:       "Favorite",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with multiple tags for same movie",
			builder: func() *MovieTag {
				return &MovieTag{
					ID:        2,
					MovieID:   "IPX-123",
					Tag:       "Must Watch",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "invalid - empty MovieID",
			builder: func() *MovieTag {
				return &MovieTag{
					ID:        3,
					MovieID:   "",
					Tag:       "Favorite",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "MovieID: cannot be empty",
		},
		{
			name: "invalid - empty Tag",
			builder: func() *MovieTag {
				return &MovieTag{
					ID:        4,
					MovieID:   "IPX-456",
					Tag:       "",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "Tag: cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mt := tt.builder()
			err := validateMovieTag(mt)

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
				assert.NotEmpty(t, mt.Tag)
			}
		})
	}
}

// TestMovieTagJSONMarshal tests JSON marshaling/unmarshaling
// AC-2.6.3: JSON marshaling/unmarshaling
func TestMovieTagJSONMarshal(t *testing.T) {
	tests := []struct {
		name string
		mt   *MovieTag
	}{
		{
			name: "marshal complete MovieTag",
			mt: &MovieTag{
				ID:        1,
				MovieID:   "IPX-123",
				Tag:       "Favorite",
				CreatedAt: time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
				UpdatedAt: time.Date(2023, 5, 16, 14, 45, 0, 0, time.UTC),
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
			var unmarshaled MovieTag
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Verify fields match
			assert.Equal(t, tt.mt.ID, unmarshaled.ID)
			assert.Equal(t, tt.mt.MovieID, unmarshaled.MovieID)
			assert.Equal(t, tt.mt.Tag, unmarshaled.Tag)
		})
	}
}

// TestMovieTagGORMTags tests GORM tag validation via reflection
// AC-2.6.3: GORM relationship tags (composite unique index)
func TestMovieTagGORMTags(t *testing.T) {
	mtType := reflect.TypeOf(MovieTag{})

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
			wantTag:   "index:idx_movie_tag,unique",
		},
		{
			name:      "MovieID field has not null constraint",
			fieldName: "MovieID",
			wantTag:   "not null",
		},
		{
			name:      "MovieID field has size constraint",
			fieldName: "MovieID",
			wantTag:   "size:50",
		},
		{
			name:      "Tag field has composite unique index",
			fieldName: "Tag",
			wantTag:   "index:idx_movie_tag,unique",
		},
		{
			name:      "Tag field has not null constraint",
			fieldName: "Tag",
			wantTag:   "not null",
		},
		{
			name:      "Tag field has size constraint",
			fieldName: "Tag",
			wantTag:   "size:100",
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

// TestMovieTagTableName tests the TableName method
func TestMovieTagTableName(t *testing.T) {
	mt := MovieTag{}
	tableName := mt.TableName()
	assert.Equal(t, "movie_tags", tableName)
}
