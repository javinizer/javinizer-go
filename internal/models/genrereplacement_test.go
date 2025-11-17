package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GenreReplacementValidationError represents a validation error for GenreReplacement
type GenreReplacementValidationError struct {
	Field   string
	Message string
}

func (e *GenreReplacementValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// validateGenreReplacement performs validation on GenreReplacement struct
// Following validation helper pattern from Stories 2.3-2.5
func validateGenreReplacement(gr *GenreReplacement) error {
	if gr.Original == "" {
		return &GenreReplacementValidationError{Field: "Original", Message: "cannot be empty"}
	}
	if gr.Replacement == "" {
		return &GenreReplacementValidationError{Field: "Replacement", Message: "cannot be empty"}
	}
	return nil
}

// TestGenreReplacementCreation tests GenreReplacement struct creation and validation
// AC-2.6.1: Valid creation, empty field validation, edge cases
func TestGenreReplacementCreation(t *testing.T) {
	tests := []struct {
		name    string
		builder func() *GenreReplacement
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid GenreReplacement with all fields",
			builder: func() *GenreReplacement {
				return &GenreReplacement{
					ID:          1,
					Original:    "Big Tits",
					Replacement: "Large Breasts",
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with Unicode genre names (Japanese)",
			builder: func() *GenreReplacement {
				return &GenreReplacement{
					ID:          2,
					Original:    "巨乳",
					Replacement: "Big Breasts",
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with very long genre names (100+ characters)",
			builder: func() *GenreReplacement {
				longName := strings.Repeat("A", 120)
				return &GenreReplacement{
					ID:          3,
					Original:    longName,
					Replacement: "Short Name",
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with same Original and Replacement (no-op translation)",
			builder: func() *GenreReplacement {
				return &GenreReplacement{
					ID:          4,
					Original:    "Cosplay",
					Replacement: "Cosplay",
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "invalid - empty Original",
			builder: func() *GenreReplacement {
				return &GenreReplacement{
					ID:          5,
					Original:    "",
					Replacement: "Valid Replacement",
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "Original: cannot be empty",
		},
		{
			name: "invalid - empty Replacement",
			builder: func() *GenreReplacement {
				return &GenreReplacement{
					ID:          6,
					Original:    "Valid Original",
					Replacement: "",
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "Replacement: cannot be empty",
		},
		{
			name: "invalid - both fields empty",
			builder: func() *GenreReplacement {
				return &GenreReplacement{
					ID:          7,
					Original:    "",
					Replacement: "",
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "Original: cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gr := tt.builder()
			err := validateGenreReplacement(gr)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			assert.NoError(t, err)
			// Verify struct fields are populated
			if !tt.wantErr {
				assert.NotEmpty(t, gr.Original)
				assert.NotEmpty(t, gr.Replacement)
			}
		})
	}
}

// TestGenreReplacementJSONMarshal tests JSON marshaling/unmarshaling
// AC-2.6.1: JSON serialization round-trip
func TestGenreReplacementJSONMarshal(t *testing.T) {
	tests := []struct {
		name string
		gr   *GenreReplacement
	}{
		{
			name: "marshal complete GenreReplacement",
			gr: &GenreReplacement{
				ID:          1,
				Original:    "Big Tits",
				Replacement: "Large Breasts",
				CreatedAt:   time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
				UpdatedAt:   time.Date(2023, 5, 16, 14, 45, 0, 0, time.UTC),
			},
		},
		{
			name: "marshal with Unicode",
			gr: &GenreReplacement{
				ID:          2,
				Original:    "痴女",
				Replacement: "Slut",
				CreatedAt:   time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
				UpdatedAt:   time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.gr)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to struct
			var unmarshaled GenreReplacement
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Verify fields match
			assert.Equal(t, tt.gr.ID, unmarshaled.ID)
			assert.Equal(t, tt.gr.Original, unmarshaled.Original)
			assert.Equal(t, tt.gr.Replacement, unmarshaled.Replacement)
			// Note: Time comparison may have precision differences, so we use Unix timestamps
			assert.Equal(t, tt.gr.CreatedAt.Unix(), unmarshaled.CreatedAt.Unix())
			assert.Equal(t, tt.gr.UpdatedAt.Unix(), unmarshaled.UpdatedAt.Unix())
		})
	}
}

// TestGenreReplacementGORMTags tests GORM tag validation via reflection
// AC-2.6.1: GORM tags validation (table name, indexes, constraints)
// Following pattern from Story 2.3
func TestGenreReplacementGORMTags(t *testing.T) {
	grType := reflect.TypeOf(GenreReplacement{})

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
			name:      "Original field has uniqueIndex tag",
			fieldName: "Original",
			wantTag:   "uniqueIndex",
		},
		{
			name:      "Original field has not null tag",
			fieldName: "Original",
			wantTag:   "not null",
		},
		{
			name:      "Replacement field has not null tag",
			fieldName: "Replacement",
			wantTag:   "not null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, found := grType.FieldByName(tt.fieldName)
			require.True(t, found, "Field %s not found", tt.fieldName)

			gormTag := field.Tag.Get("gorm")
			assert.Contains(t, gormTag, tt.wantTag, "Field %s missing expected GORM tag: %s", tt.fieldName, tt.wantTag)
		})
	}
}

// TestGenreReplacementTableName tests the TableName method is not defined (uses default)
// GenreReplacement doesn't have a custom TableName method, so GORM uses default "genre_replacements"
func TestGenreReplacementTableName(t *testing.T) {
	// GenreReplacement struct doesn't implement TableName() method
	// GORM will use default table name: "genre_replacements"
	// This test verifies the expected behavior
	gr := &GenreReplacement{}

	// We can't directly test the table name without a DB connection,
	// but we can verify the struct doesn't have the method
	grType := reflect.TypeOf(gr)
	_, hasTableName := grType.MethodByName("TableName")
	assert.False(t, hasTableName, "GenreReplacement should not have custom TableName method")
}
