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

// TestActressTranslationGORMTags tests GORM tag validation via reflection
func TestActressTranslationGORMTags(t *testing.T) {
	t.Parallel()
	atType := reflect.TypeOf(ActressTranslation{})

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
			name:      "ActressID field has composite unique index",
			fieldName: "ActressID",
			wantTag:   "index:idx_actress_translation_actress_language,unique",
		},
		{
			name:      "ActressID field has not null constraint",
			fieldName: "ActressID",
			wantTag:   "not null",
		},
		{
			name:      "Language field has composite unique index",
			fieldName: "Language",
			wantTag:   "index:idx_actress_translation_actress_language,unique",
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
			field, found := atType.FieldByName(tt.fieldName)
			require.True(t, found, "Field %s not found", tt.fieldName)

			gormTag := field.Tag.Get("gorm")
			assert.Contains(t, gormTag, tt.wantTag, "Field %s missing expected GORM tag: %s", tt.fieldName, tt.wantTag)
		})
	}
}

// TestActressTranslationTableName tests the TableName method
func TestActressTranslationTableName(t *testing.T) {
	t.Parallel()
	at := ActressTranslation{}
	tableName := at.TableName()
	assert.Equal(t, "actress_translations", tableName)
}

// TestActressTranslationJSONRoundTrip tests JSON marshaling/unmarshaling
func TestActressTranslationJSONRoundTrip(t *testing.T) {
	t.Parallel()
	at := ActressTranslation{
		ID:           1,
		ActressID:    99,
		Language:     "en",
		FirstName:    "Yui",
		LastName:     "Hatano",
		JapaneseName: "波多野結衣",
		DisplayName:  "Hatano Yui",
		SourceName:   "dmm",
		CreatedAt:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2024, 1, 16, 14, 45, 0, 0, time.UTC),
	}

	data, err := json.Marshal(at)
	require.NoError(t, err)

	var unmarshaled ActressTranslation
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, at.ID, unmarshaled.ID)
	assert.Equal(t, at.ActressID, unmarshaled.ActressID)
	assert.Equal(t, at.Language, unmarshaled.Language)
	assert.Equal(t, at.FirstName, unmarshaled.FirstName)
	assert.Equal(t, at.LastName, unmarshaled.LastName)
	assert.Equal(t, at.JapaneseName, unmarshaled.JapaneseName)
	assert.Equal(t, at.DisplayName, unmarshaled.DisplayName)
	assert.Equal(t, at.SourceName, unmarshaled.SourceName)
}

// TestActressTranslationFieldsExist verifies all expected fields exist on the struct
func TestActressTranslationFieldsExist(t *testing.T) {
	t.Parallel()
	atType := reflect.TypeOf(ActressTranslation{})
	expectedFields := []string{"ID", "ActressID", "Language", "FirstName", "LastName", "JapaneseName", "DisplayName", "SourceName", "CreatedAt", "UpdatedAt"}

	for _, fieldName := range expectedFields {
		_, found := atType.FieldByName(fieldName)
		assert.True(t, found, "ActressTranslation missing expected field: %s", fieldName)
	}
}
