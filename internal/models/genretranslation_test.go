package models

// All tests in this package are safe for parallel execution (no shared state).
// Pure validation logic with no database writes or global config modifications.

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenreTranslationGORMTags tests GORM tag validation via reflection
func TestGenreTranslationGORMTags(t *testing.T) {
	t.Parallel()
	gtType := reflect.TypeOf(GenreTranslation{})

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
			name:      "GenreID field has composite unique index",
			fieldName: "GenreID",
			wantTag:   "index:idx_genre_translation_genre_language,unique",
		},
		{
			name:      "GenreID field has not null constraint",
			fieldName: "GenreID",
			wantTag:   "not null",
		},
		{
			name:      "Language field has composite unique index",
			fieldName: "Language",
			wantTag:   "index:idx_genre_translation_genre_language,unique",
		},
		{
			name:      "Language field has size constraint",
			fieldName: "Language",
			wantTag:   "size:5",
		},
		{
			name:      "Language field has not null constraint",
			fieldName: "Language",
			wantTag:   "not null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			field, found := gtType.FieldByName(tt.fieldName)
			require.True(t, found, "Field %s not found", tt.fieldName)

			gormTag := field.Tag.Get("gorm")
			assert.Contains(t, gormTag, tt.wantTag, "Field %s missing expected GORM tag: %s", tt.fieldName, tt.wantTag)
		})
	}
}

// TestGenreTranslationTableName tests the TableName method
func TestGenreTranslationTableName(t *testing.T) {
	t.Parallel()
	gt := GenreTranslation{}
	tableName := gt.TableName()
	assert.Equal(t, "genre_translations", tableName)
}

// TestGenreTranslationJSONRoundTrip tests JSON marshaling/unmarshaling
func TestGenreTranslationJSONRoundTrip(t *testing.T) {
	t.Parallel()
	gt := GenreTranslation{
		ID:         1,
		GenreID:    42,
		Language:   "en",
		Name:       "Beautiful Woman",
		SourceName: "r18dev",
		CreatedAt:  time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 1, 16, 14, 45, 0, 0, time.UTC),
	}

	data, err := json.Marshal(gt)
	require.NoError(t, err)

	var unmarshaled GenreTranslation
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, gt.ID, unmarshaled.ID)
	assert.Equal(t, gt.GenreID, unmarshaled.GenreID)
	assert.Equal(t, gt.Language, unmarshaled.Language)
	assert.Equal(t, gt.Name, unmarshaled.Name)
	assert.Equal(t, gt.SourceName, unmarshaled.SourceName)
}

// TestGenreTranslationFieldsExist verifies all expected fields exist on the struct
func TestGenreTranslationFieldsExist(t *testing.T) {
	t.Parallel()
	gtType := reflect.TypeOf(GenreTranslation{})
	expectedFields := []string{"ID", "GenreID", "Language", "Name", "SourceName", "CreatedAt", "UpdatedAt"}

	for _, fieldName := range expectedFields {
		_, found := gtType.FieldByName(fieldName)
		assert.True(t, found, "GenreTranslation missing expected field: %s", fieldName)
	}
}
