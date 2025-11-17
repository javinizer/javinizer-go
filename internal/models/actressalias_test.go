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

// ActressAliasValidationError represents a validation error for ActressAlias
type ActressAliasValidationError struct {
	Field   string
	Message string
}

func (e *ActressAliasValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// validateActressAlias performs validation on ActressAlias struct
func validateActressAlias(aa *ActressAlias) error {
	if aa.AliasName == "" {
		return &ActressAliasValidationError{Field: "AliasName", Message: "cannot be empty"}
	}
	if aa.CanonicalName == "" {
		return &ActressAliasValidationError{Field: "CanonicalName", Message: "cannot be empty"}
	}
	return nil
}

// TestActressAliasCreation tests ActressAlias struct creation and validation
// AC-2.6.4: Valid creation, empty AliasName validation, Unicode, long names
func TestActressAliasCreation(t *testing.T) {
	tests := []struct {
		name    string
		builder func() *ActressAlias
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid ActressAlias with all fields",
			builder: func() *ActressAlias {
				return &ActressAlias{
					ID:            1,
					AliasName:     "Yui Hatano",
					CanonicalName: "Hatano Yui",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with Unicode alias names (Japanese)",
			builder: func() *ActressAlias {
				return &ActressAlias{
					ID:            2,
					AliasName:     "波多野結衣",
					CanonicalName: "Hatano Yui",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with Unicode alias names (Korean)",
			builder: func() *ActressAlias {
				return &ActressAlias{
					ID:            3,
					AliasName:     "하타노 유이",
					CanonicalName: "Hatano Yui",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with very long AliasName (200+ characters)",
			builder: func() *ActressAlias {
				longName := strings.Repeat("A", 220)
				return &ActressAlias{
					ID:            4,
					AliasName:     longName,
					CanonicalName: "Short Name",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with multiple aliases for same actress",
			builder: func() *ActressAlias {
				return &ActressAlias{
					ID:            5,
					AliasName:     "Yui H",
					CanonicalName: "Hatano Yui",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "invalid - empty AliasName",
			builder: func() *ActressAlias {
				return &ActressAlias{
					ID:            6,
					AliasName:     "",
					CanonicalName: "Hatano Yui",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "AliasName: cannot be empty",
		},
		{
			name: "invalid - empty CanonicalName",
			builder: func() *ActressAlias {
				return &ActressAlias{
					ID:            7,
					AliasName:     "Yui Hatano",
					CanonicalName: "",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "CanonicalName: cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			aa := tt.builder()
			err := validateActressAlias(aa)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			assert.NoError(t, err)
			if !tt.wantErr {
				assert.NotEmpty(t, aa.AliasName)
				assert.NotEmpty(t, aa.CanonicalName)
			}
		})
	}
}

// TestActressAliasJSONMarshal tests JSON marshaling/unmarshaling
// AC-2.6.4: JSON marshaling/unmarshaling
func TestActressAliasJSONMarshal(t *testing.T) {
	tests := []struct {
		name string
		aa   *ActressAlias
	}{
		{
			name: "marshal complete ActressAlias",
			aa: &ActressAlias{
				ID:            1,
				AliasName:     "Yui Hatano",
				CanonicalName: "Hatano Yui",
				CreatedAt:     time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
				UpdatedAt:     time.Date(2023, 5, 16, 14, 45, 0, 0, time.UTC),
			},
		},
		{
			name: "marshal with Unicode",
			aa: &ActressAlias{
				ID:            2,
				AliasName:     "波多野結衣",
				CanonicalName: "Hatano Yui",
				CreatedAt:     time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
				UpdatedAt:     time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Marshal to JSON
			data, err := json.Marshal(tt.aa)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to struct
			var unmarshaled ActressAlias
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Verify fields match
			assert.Equal(t, tt.aa.ID, unmarshaled.ID)
			assert.Equal(t, tt.aa.AliasName, unmarshaled.AliasName)
			assert.Equal(t, tt.aa.CanonicalName, unmarshaled.CanonicalName)
		})
	}
}

// TestActressAliasGORMTags tests GORM tag validation via reflection
// AC-2.6.4: GORM relationship tags
func TestActressAliasGORMTags(t *testing.T) {
	aaType := reflect.TypeOf(ActressAlias{})

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
			name:      "AliasName field has uniqueIndex tag",
			fieldName: "AliasName",
			wantTag:   "uniqueIndex",
		},
		{
			name:      "AliasName field has not null tag",
			fieldName: "AliasName",
			wantTag:   "not null",
		},
		{
			name:      "CanonicalName field has index tag",
			fieldName: "CanonicalName",
			wantTag:   "index",
		},
		{
			name:      "CanonicalName field has not null tag",
			fieldName: "CanonicalName",
			wantTag:   "not null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			field, found := aaType.FieldByName(tt.fieldName)
			require.True(t, found, "Field %s not found", tt.fieldName)

			gormTag := field.Tag.Get("gorm")
			assert.Contains(t, gormTag, tt.wantTag, "Field %s missing expected GORM tag: %s", tt.fieldName, tt.wantTag)
		})
	}
}
