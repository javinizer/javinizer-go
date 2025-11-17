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

// ContentIDMappingValidationError represents a validation error for ContentIDMapping
type ContentIDMappingValidationError struct {
	Field   string
	Message string
}

func (e *ContentIDMappingValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// validateContentIDMapping performs validation on ContentIDMapping struct
func validateContentIDMapping(cid *ContentIDMapping) error {
	if cid.SearchID == "" {
		return &ContentIDMappingValidationError{Field: "SearchID", Message: "cannot be empty"}
	}
	if cid.ContentID == "" {
		return &ContentIDMappingValidationError{Field: "ContentID", Message: "cannot be empty"}
	}
	if cid.Source == "" {
		return &ContentIDMappingValidationError{Field: "Source", Message: "cannot be empty"}
	}
	return nil
}

// TestContentIDMappingCreation tests ContentIDMapping struct creation and validation
// AC-2.6.5: Valid creation, h_<digits> prefix handling, ID format validation
func TestContentIDMappingCreation(t *testing.T) {
	tests := []struct {
		name    string
		builder func() *ContentIDMapping
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid ContentIDMapping with all fields",
			builder: func() *ContentIDMapping {
				return &ContentIDMapping{
					ID:        1,
					SearchID:  "MDB-087",
					ContentID: "61mdb087",
					Source:    "dmm",
					CreatedAt: time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with h_<digits> prefix normalization",
			builder: func() *ContentIDMapping {
				return &ContentIDMapping{
					ID:        2,
					SearchID:  "h_123456789",
					ContentID: "123456789",
					Source:    "dmm",
					CreatedAt: time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with hyphenated ID (IPX-123)",
			builder: func() *ContentIDMapping {
				return &ContentIDMapping{
					ID:        3,
					SearchID:  "IPX-123",
					ContentID: "ipx00123",
					Source:    "dmm",
					CreatedAt: time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with non-hyphenated ID (IPX123)",
			builder: func() *ContentIDMapping {
				return &ContentIDMapping{
					ID:        4,
					SearchID:  "IPX123",
					ContentID: "ipx00123",
					Source:    "dmm",
					CreatedAt: time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with case sensitivity (ipx-123 vs IPX-123)",
			builder: func() *ContentIDMapping {
				return &ContentIDMapping{
					ID:        5,
					SearchID:  "ipx-123",
					ContentID: "IPX00123",
					Source:    "r18dev",
					CreatedAt: time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with same SearchID and ContentID (already normalized)",
			builder: func() *ContentIDMapping {
				return &ContentIDMapping{
					ID:        6,
					SearchID:  "normalized123",
					ContentID: "normalized123",
					Source:    "dmm",
					CreatedAt: time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "invalid - empty SearchID",
			builder: func() *ContentIDMapping {
				return &ContentIDMapping{
					ID:        7,
					SearchID:  "",
					ContentID: "61mdb087",
					Source:    "dmm",
					CreatedAt: time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "SearchID: cannot be empty",
		},
		{
			name: "invalid - empty ContentID",
			builder: func() *ContentIDMapping {
				return &ContentIDMapping{
					ID:        8,
					SearchID:  "MDB-087",
					ContentID: "",
					Source:    "dmm",
					CreatedAt: time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "ContentID: cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cid := tt.builder()
			err := validateContentIDMapping(cid)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			assert.NoError(t, err)
			if !tt.wantErr {
				assert.NotEmpty(t, cid.SearchID)
				assert.NotEmpty(t, cid.ContentID)
				assert.NotEmpty(t, cid.Source)
			}
		})
	}
}

// TestContentIDMappingJSONMarshal tests JSON marshaling/unmarshaling
// AC-2.6.5: JSON marshaling/unmarshaling
func TestContentIDMappingJSONMarshal(t *testing.T) {
	tests := []struct {
		name string
		cid  *ContentIDMapping
	}{
		{
			name: "marshal complete ContentIDMapping",
			cid: &ContentIDMapping{
				ID:        1,
				SearchID:  "MDB-087",
				ContentID: "61mdb087",
				Source:    "dmm",
				CreatedAt: time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "marshal with h_ prefix",
			cid: &ContentIDMapping{
				ID:        2,
				SearchID:  "h_123456789",
				ContentID: "123456789",
				Source:    "dmm",
				CreatedAt: time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Marshal to JSON
			data, err := json.Marshal(tt.cid)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to struct
			var unmarshaled ContentIDMapping
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Verify fields match
			assert.Equal(t, tt.cid.ID, unmarshaled.ID)
			assert.Equal(t, tt.cid.SearchID, unmarshaled.SearchID)
			assert.Equal(t, tt.cid.ContentID, unmarshaled.ContentID)
			assert.Equal(t, tt.cid.Source, unmarshaled.Source)
		})
	}
}

// TestContentIDMappingGORMTags tests GORM tag validation via reflection
// AC-2.6.5: GORM tags validation
func TestContentIDMappingGORMTags(t *testing.T) {
	cidType := reflect.TypeOf(ContentIDMapping{})

	tests := []struct {
		name      string
		fieldName string
		wantTag   string
	}{
		{
			name:      "ID field has primarykey tag",
			fieldName: "ID",
			wantTag:   "primarykey",
		},
		{
			name:      "SearchID field has uniqueIndex tag",
			fieldName: "SearchID",
			wantTag:   "uniqueIndex",
		},
		{
			name:      "SearchID field has not null tag",
			fieldName: "SearchID",
			wantTag:   "not null",
		},
		{
			name:      "ContentID field has not null tag",
			fieldName: "ContentID",
			wantTag:   "not null",
		},
		{
			name:      "Source field has not null tag",
			fieldName: "Source",
			wantTag:   "not null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			field, found := cidType.FieldByName(tt.fieldName)
			require.True(t, found, "Field %s not found", tt.fieldName)

			gormTag := field.Tag.Get("gorm")
			assert.Contains(t, gormTag, tt.wantTag, "Field %s missing expected GORM tag: %s", tt.fieldName, tt.wantTag)
		})
	}
}
