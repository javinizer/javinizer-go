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

// HistoryValidationError represents a validation error for History
type HistoryValidationError struct {
	Field   string
	Message string
}

func (e *HistoryValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// validateHistory performs validation on History struct
// Following validation helper pattern from Stories 2.3-2.5
func validateHistory(h *History) error {
	if h.Operation == "" {
		return &HistoryValidationError{Field: "Operation", Message: "cannot be empty"}
	}
	validOperations := map[string]bool{"scrape": true, "organize": true, "download": true, "nfo": true}
	if !validOperations[h.Operation] {
		return &HistoryValidationError{Field: "Operation", Message: "invalid operation type"}
	}
	return nil
}

// TestHistoryCreation tests History struct creation and validation
// AC-2.6.2: Valid creation, operation serialization, edge cases
func TestHistoryCreation(t *testing.T) {
	tests := []struct {
		name    string
		builder func() *History
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid History with all fields",
			builder: func() *History {
				return &History{
					ID:           1,
					MovieID:      "IPX-123",
					Operation:    "scrape",
					OriginalPath: "/original/path/movie.mp4",
					NewPath:      "/new/path/IPX-123.mp4",
					Status:       "success",
					ErrorMessage: "",
					Metadata:     `{"scraper":"r18dev","duration":120}`,
					DryRun:       false,
					CreatedAt:    time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with empty Metadata (no metadata for operation)",
			builder: func() *History {
				return &History{
					ID:           2,
					MovieID:      "IPX-456",
					Operation:    "organize",
					OriginalPath: "/source/file.mp4",
					NewPath:      "/dest/file.mp4",
					Status:       "success",
					Metadata:     "",
					DryRun:       false,
					CreatedAt:    time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with nested JSON in Metadata",
			builder: func() *History {
				return &History{
					ID:           3,
					MovieID:      "IPX-789",
					Operation:    "download",
					OriginalPath: "",
					NewPath:      "/downloads/cover.jpg",
					Status:       "success",
					Metadata:     `{"urls":{"cover":"https://example.com/cover.jpg","poster":"https://example.com/poster.jpg"},"size":1024000}`,
					DryRun:       false,
					CreatedAt:    time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with very long paths (500+ characters)",
			builder: func() *History {
				longPath := "/very/long/path/" + strings.Repeat("subdir/", 50) + "movie.mp4"
				return &History{
					ID:           4,
					MovieID:      "IPX-999",
					Operation:    "organize",
					OriginalPath: longPath,
					NewPath:      longPath + ".new",
					Status:       "success",
					Metadata:     "",
					DryRun:       false,
					CreatedAt:    time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with special characters in paths (Unicode, spaces)",
			builder: func() *History {
				return &History{
					ID:           5,
					MovieID:      "IPX-111",
					Operation:    "scrape",
					OriginalPath: "/path/with spaces/and 日本語/movie.mp4",
					NewPath:      "/new/path/with spaces/IPX-111.mp4",
					Status:       "success",
					Metadata:     "",
					DryRun:       false,
					CreatedAt:    time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "valid with failed operation",
			builder: func() *History {
				return &History{
					ID:           6,
					MovieID:      "IPX-222",
					Operation:    "download",
					OriginalPath: "",
					NewPath:      "/downloads/failed.jpg",
					Status:       "failed",
					ErrorMessage: "HTTP 404: Not Found",
					Metadata:     `{"url":"https://example.com/notfound.jpg"}`,
					DryRun:       false,
					CreatedAt:    time.Now(),
				}
			},
			wantErr: false,
		},
		{
			name: "invalid - empty Operation",
			builder: func() *History {
				return &History{
					ID:           7,
					MovieID:      "IPX-333",
					Operation:    "",
					OriginalPath: "/path/file.mp4",
					NewPath:      "/new/path/file.mp4",
					Status:       "success",
					Metadata:     "",
					DryRun:       false,
					CreatedAt:    time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "Operation: cannot be empty",
		},
		{
			name: "invalid - invalid Operation type",
			builder: func() *History {
				return &History{
					ID:           8,
					MovieID:      "IPX-444",
					Operation:    "invalid_operation",
					OriginalPath: "/path/file.mp4",
					NewPath:      "/new/path/file.mp4",
					Status:       "success",
					Metadata:     "",
					DryRun:       false,
					CreatedAt:    time.Now(),
				}
			},
			wantErr: true,
			errMsg:  "Operation: invalid operation type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := tt.builder()
			err := validateHistory(h)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			assert.NoError(t, err)
			// Verify critical fields are populated
			if !tt.wantErr {
				assert.NotEmpty(t, h.Operation)
				assert.NotEmpty(t, h.Status)
			}
		})
	}
}

// TestHistoryJSONMarshal tests JSON marshaling/unmarshaling for Metadata field
// AC-2.6.2: JSON marshaling for Metadata field (JSONB), operation serialization
func TestHistoryJSONMarshal(t *testing.T) {
	tests := []struct {
		name string
		h    *History
	}{
		{
			name: "marshal complete History",
			h: &History{
				ID:           1,
				MovieID:      "IPX-123",
				Operation:    "scrape",
				OriginalPath: "/original/path/movie.mp4",
				NewPath:      "/new/path/IPX-123.mp4",
				Status:       "success",
				ErrorMessage: "",
				Metadata:     `{"scraper":"r18dev","duration":120}`,
				DryRun:       false,
				CreatedAt:    time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "marshal with empty Metadata",
			h: &History{
				ID:           2,
				MovieID:      "IPX-456",
				Operation:    "organize",
				OriginalPath: "/source/file.mp4",
				NewPath:      "/dest/file.mp4",
				Status:       "success",
				Metadata:     "",
				DryRun:       false,
				CreatedAt:    time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "marshal with nested JSON Metadata",
			h: &History{
				ID:           3,
				MovieID:      "IPX-789",
				Operation:    "download",
				OriginalPath: "",
				NewPath:      "/downloads/cover.jpg",
				Status:       "success",
				Metadata:     `{"urls":{"cover":"https://example.com/cover.jpg"},"size":1024000}`,
				DryRun:       false,
				CreatedAt:    time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Marshal to JSON
			data, err := json.Marshal(tt.h)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to struct
			var unmarshaled History
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Verify fields match
			assert.Equal(t, tt.h.ID, unmarshaled.ID)
			assert.Equal(t, tt.h.MovieID, unmarshaled.MovieID)
			assert.Equal(t, tt.h.Operation, unmarshaled.Operation)
			assert.Equal(t, tt.h.Metadata, unmarshaled.Metadata)
			assert.Equal(t, tt.h.Status, unmarshaled.Status)
		})
	}
}

// TestHistoryOperationTypes tests all valid operation types
// AC-2.6.2: Operation serialization (move, copy, organize, scrape)
func TestHistoryOperationTypes(t *testing.T) {
	operations := []string{"scrape", "organize", "download", "nfo"}

	for _, op := range operations {
		t.Run("operation_"+op, func(t *testing.T) {
			t.Parallel()
			h := &History{
				ID:           1,
				MovieID:      "IPX-123",
				Operation:    op,
				OriginalPath: "/path/file.mp4",
				NewPath:      "/new/path/file.mp4",
				Status:       "success",
				DryRun:       false,
				CreatedAt:    time.Now(),
			}

			err := validateHistory(h)
			assert.NoError(t, err)
			assert.Equal(t, op, h.Operation)
		})
	}
}

// TestHistoryGORMTags tests GORM tag validation via reflection
// AC-2.6.2: GORM tags validation
func TestHistoryGORMTags(t *testing.T) {
	hType := reflect.TypeOf(History{})

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
			name:      "MovieID field has index tag",
			fieldName: "MovieID",
			wantTag:   "index",
		},
		{
			name:      "ErrorMessage field has type:text tag",
			fieldName: "ErrorMessage",
			wantTag:   "type:text",
		},
		{
			name:      "Metadata field has type:json tag",
			fieldName: "Metadata",
			wantTag:   "type:json",
		},
		{
			name:      "CreatedAt field has index tag",
			fieldName: "CreatedAt",
			wantTag:   "index",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			field, found := hType.FieldByName(tt.fieldName)
			require.True(t, found, "Field %s not found", tt.fieldName)

			gormTag := field.Tag.Get("gorm")
			assert.Contains(t, gormTag, tt.wantTag, "Field %s missing expected GORM tag: %s", tt.fieldName, tt.wantTag)
		})
	}
}

// TestHistoryTableName tests the TableName method
func TestHistoryTableName(t *testing.T) {
	h := History{}
	tableName := h.TableName()
	assert.Equal(t, "history", tableName)
}
