package models

// All tests in this package are safe for parallel execution (no shared state).
// Pure validation logic with no database writes or global config modifications.
// Reference: Architecture Decision 8 (concurrent testing with -race flag)

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Embedded golden files for JSON marshaling tests
//
//go:embed testdata/actress_full.json.golden
var actressFullGolden []byte

//go:embed testdata/actress_with_aliases.json.golden
var actressWithAliasesGolden []byte

// ValidationError represents a validation error for test purposes
type ActressValidationError struct {
	Field   string
	Message string
}

func (e *ActressValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// validateActress validates an Actress struct for test purposes
// This is a test helper following the pattern from movie_test.go
func validateActress(a *Actress) error {
	// Note: In Story 2.4, we're not implementing full validation on the model itself
	// This is a test-only validation helper to verify business rules
	// Actual model validation should be implemented in Epic 3

	if a.JapaneseName == "" {
		return &ActressValidationError{Field: "JapaneseName", Message: "cannot be empty"}
	}

	// DMMID must be non-negative if provided (0 means not set)
	if a.DMMID < 0 {
		return &ActressValidationError{Field: "DMMID", Message: "cannot be negative"}
	}

	// Name length validation (business rule: max 200 chars)
	if len(a.JapaneseName) > 200 {
		return &ActressValidationError{Field: "JapaneseName", Message: "exceeds 200 characters"}
	}

	return nil
}

// isDuplicateByDMMID checks if two actresses are duplicates based on DMMID
// Two actresses with the same non-zero DMMID are considered duplicates
// This is the core deduplication logic being tested
func isDuplicateByDMMID(a1, a2 *Actress) bool {
	// If either DMMID is 0 (not set), cannot determine duplication
	if a1.DMMID == 0 || a2.DMMID == 0 {
		return false
	}
	// Same DMMID = duplicate
	return a1.DMMID == a2.DMMID
}

// TestActressCreation tests Actress struct creation with various field configurations (AC-2.4.1)
func TestActressCreation(t *testing.T) {
	tests := []struct {
		name    string
		builder func() *Actress
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid actress with DMMID",
			builder: func() *Actress {
				return &Actress{
					ID:           1,
					DMMID:        123456,
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
					ThumbURL:     "https://example.com/thumb.jpg",
					Aliases:      "",
					CreatedAt:    time.Date(2023, 1, 15, 10, 30, 0, 0, time.UTC),
					UpdatedAt:    time.Date(2023, 1, 15, 10, 30, 0, 0, time.UTC),
				}
			},
			wantErr: false,
		},
		{
			name: "valid actress without DMMID (zero value allowed)",
			builder: func() *Actress {
				return &Actress{
					ID:           2,
					DMMID:        0, // Zero DMMID is allowed (actress doesn't have DMM ID)
					FirstName:    "Test",
					LastName:     "Actress",
					JapaneseName: "テスト女優",
					ThumbURL:     "",
					Aliases:      "",
					CreatedAt:    time.Date(2023, 2, 20, 14, 0, 0, 0, time.UTC),
					UpdatedAt:    time.Date(2023, 2, 20, 14, 0, 0, 0, time.UTC),
				}
			},
			wantErr: false,
		},
		{
			name: "valid actress with all fields populated",
			builder: func() *Actress {
				return &Actress{
					ID:           3,
					DMMID:        789012,
					FirstName:    "Full",
					LastName:     "Name",
					JapaneseName: "完全名前",
					ThumbURL:     "https://example.com/full.jpg",
					Aliases:      "Alias1|Alias2|別名",
					CreatedAt:    time.Date(2023, 3, 10, 9, 0, 0, 0, time.UTC),
					UpdatedAt:    time.Date(2023, 3, 10, 9, 0, 0, 0, time.UTC),
				}
			},
			wantErr: false,
		},
		{
			name: "valid actress with minimal fields (JapaneseName only)",
			builder: func() *Actress {
				return &Actress{
					JapaneseName: "最小名前",
				}
			},
			wantErr: false,
		},
		{
			name: "invalid actress with empty JapaneseName",
			builder: func() *Actress {
				return &Actress{
					FirstName: "Test",
					LastName:  "Actress",
				}
			},
			wantErr: true,
			errMsg:  "JapaneseName: cannot be empty",
		},
		{
			name: "invalid actress with negative DMMID",
			builder: func() *Actress {
				return &Actress{
					DMMID:        -1,
					JapaneseName: "Test",
				}
			},
			wantErr: true,
			errMsg:  "DMMID: cannot be negative",
		},
		{
			name: "invalid actress with JapaneseName exceeding 200 chars",
			builder: func() *Actress {
				longName := strings.Repeat("あ", 201) // 201 characters
				return &Actress{
					JapaneseName: longName,
				}
			},
			wantErr: true,
			errMsg:  "JapaneseName: exceeds 200 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			actress := tt.builder()

			// Validate actress
			err := validateActress(actress)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			assert.NoError(t, err)
			// Verify basic struct properties
			if actress.DMMID != 0 {
				assert.Greater(t, actress.DMMID, 0, "DMMID should be positive when set")
			}
			assert.NotEmpty(t, actress.JapaneseName, "JapaneseName should not be empty for valid actress")
		})
	}
}

// TestDMMIDDeduplication tests DMMID-based deduplication logic (AC-2.4.2)
func TestDMMIDDeduplication(t *testing.T) {
	tests := []struct {
		name        string
		actress1    *Actress
		actress2    *Actress
		isDuplicate bool
		description string
	}{
		{
			name: "same DMMID different names - detected as duplicates",
			actress1: &Actress{
				DMMID:        12345,
				JapaneseName: "波多野結衣",
				FirstName:    "",
				LastName:     "",
			},
			actress2: &Actress{
				DMMID:        12345,
				JapaneseName: "Yui Hatano",
				FirstName:    "Yui",
				LastName:     "Hatano",
			},
			isDuplicate: true,
			description: "r18dev and dmm return same DMMID with different names",
		},
		{
			name: "same name different DMMID - NOT duplicates",
			actress1: &Actress{
				DMMID:        12345,
				JapaneseName: "Test Actress",
			},
			actress2: &Actress{
				DMMID:        67890,
				JapaneseName: "Test Actress",
			},
			isDuplicate: false,
			description: "Different actresses with same name",
		},
		{
			name: "same name both zero DMMID - cannot determine (allowed)",
			actress1: &Actress{
				DMMID:        0,
				JapaneseName: "Unknown Actress",
			},
			actress2: &Actress{
				DMMID:        0,
				JapaneseName: "Unknown Actress",
			},
			isDuplicate: false,
			description: "No DMMID means we cannot determine if duplicate",
		},
		{
			name: "one has DMMID one doesn't - cannot determine",
			actress1: &Actress{
				DMMID:        12345,
				JapaneseName: "Test",
			},
			actress2: &Actress{
				DMMID:        0,
				JapaneseName: "Test",
			},
			isDuplicate: false,
			description: "Cannot determine duplication when one lacks DMMID",
		},
		{
			name: "r18dev and dmm scraper scenario - same actress",
			actress1: &Actress{
				DMMID:        123456,
				JapaneseName: "Yui Hatano",
				FirstName:    "Yui",
				LastName:     "Hatano",
			},
			actress2: &Actress{
				DMMID:        123456,
				JapaneseName: "波多野結衣",
				FirstName:    "",
				LastName:     "",
			},
			isDuplicate: true,
			description: "Real-world scenario: r18dev (English) and dmm (Japanese) return same DMMID",
		},
		{
			name: "different DMMID different names - NOT duplicates",
			actress1: &Actress{
				DMMID:        11111,
				JapaneseName: "Actress One",
			},
			actress2: &Actress{
				DMMID:        22222,
				JapaneseName: "Actress Two",
			},
			isDuplicate: false,
			description: "Completely different actresses",
		},
		{
			name: "identical DMMID - duplicates",
			actress1: &Actress{
				DMMID:        99999,
				JapaneseName: "Same Actress",
				FirstName:    "First",
			},
			actress2: &Actress{
				DMMID:        99999,
				JapaneseName: "Same Actress",
				FirstName:    "First",
			},
			isDuplicate: true,
			description: "Exact match including names",
		},
		{
			name: "both zero DMMID different names - cannot determine",
			actress1: &Actress{
				DMMID:        0,
				JapaneseName: "Actress A",
			},
			actress2: &Actress{
				DMMID:        0,
				JapaneseName: "Actress B",
			},
			isDuplicate: false,
			description: "Different names but no DMMID to verify",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isDuplicateByDMMID(tt.actress1, tt.actress2)
			assert.Equal(t, tt.isDuplicate, result, tt.description)
		})
	}
}

// TestGORMUniqueConstraintOnDMMID validates GORM uniqueIndex tag on DMMID field (AC-2.4.2)
func TestGORMUniqueConstraintOnDMMID(t *testing.T) {
	actressType := reflect.TypeOf(Actress{})
	field, found := actressType.FieldByName("DMMID")
	require.True(t, found, "DMMID field should exist in Actress struct")

	gormTag := field.Tag.Get("gorm")
	assert.Contains(t, gormTag, "uniqueIndex", "DMMID field must have uniqueIndex tag for deduplication")

	// Document the behavior
	t.Log("✓ DMMID has uniqueIndex tag - prevents duplicate actress entries in database")
	t.Log("✓ Business rule: Two actresses with same DMMID are the same person")
	t.Log("✓ Scraper scenario: r18dev and dmm may return same actress with different names but same DMMID")
	t.Log("✓ Zero DMMID allowed: Some actresses may not have DMM ID (cannot deduplicate)")
}

// TestActressJSONMarshaling tests Actress struct to JSON conversion (AC-2.4.3)
func TestActressJSONMarshaling(t *testing.T) {
	tests := []struct {
		name         string
		actress      *Actress
		goldenFile   []byte
		useGolden    bool
		validateJSON func(*testing.T, []byte)
	}{
		{
			name: "marshal full actress to JSON",
			actress: &Actress{
				ID:           1,
				DMMID:        123456,
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
				ThumbURL:     "https://example.com/actress_thumb.jpg",
				Aliases:      "Yui H.|ゆい",
				CreatedAt:    time.Date(2023, 1, 15, 10, 30, 0, 0, time.UTC),
				UpdatedAt:    time.Date(2023, 1, 15, 10, 30, 0, 0, time.UTC),
			},
			goldenFile: actressFullGolden,
			useGolden:  true,
		},
		{
			name: "marshal actress with aliases to JSON",
			actress: &Actress{
				ID:           2,
				DMMID:        789012,
				FirstName:    "Test",
				LastName:     "Actress",
				JapaneseName: "テスト女優",
				ThumbURL:     "https://example.com/test_actress.jpg",
				Aliases:      "Alias One|Alias Two|テストさん",
				CreatedAt:    time.Date(2023, 2, 20, 14, 0, 0, 0, time.UTC),
				UpdatedAt:    time.Date(2023, 2, 20, 14, 0, 0, 0, time.UTC),
			},
			goldenFile: actressWithAliasesGolden,
			useGolden:  true,
		},
		{
			name: "marshal actress with zero DMMID",
			actress: &Actress{
				ID:           3,
				DMMID:        0, // Zero DMMID should serialize as 0
				JapaneseName: "No DMM ID",
				Aliases:      "",
			},
			validateJSON: func(t *testing.T, data []byte) {
				var result map[string]interface{}
				err := json.Unmarshal(data, &result)
				require.NoError(t, err)

				// DMMID should be 0 in JSON
				dmmID, ok := result["dmm_id"].(float64)
				require.True(t, ok, "dmm_id should be present in JSON")
				assert.Equal(t, float64(0), dmmID, "Zero DMMID should serialize as 0")
			},
		},
		{
			name: "marshal actress with empty aliases",
			actress: &Actress{
				ID:           4,
				DMMID:        999,
				JapaneseName: "Single Name",
				Aliases:      "", // Empty aliases should serialize as empty string
			},
			validateJSON: func(t *testing.T, data []byte) {
				var result map[string]interface{}
				err := json.Unmarshal(data, &result)
				require.NoError(t, err)

				aliases, ok := result["aliases"].(string)
				require.True(t, ok, "aliases should be string in JSON")
				assert.Empty(t, aliases, "Empty aliases should serialize as empty string")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Marshal actress to JSON
			actual, err := json.MarshalIndent(tt.actress, "", "  ")
			require.NoError(t, err, "Failed to marshal actress to JSON")

			if tt.useGolden {
				// Compare with golden file
				var expectedMap, actualMap map[string]interface{}
				err = json.Unmarshal(tt.goldenFile, &expectedMap)
				require.NoError(t, err, "Failed to unmarshal golden file")
				err = json.Unmarshal(actual, &actualMap)
				require.NoError(t, err, "Failed to unmarshal actual JSON")

				// Compare key fields (ignoring timestamps that may differ slightly)
				assert.Equal(t, expectedMap["id"], actualMap["id"])
				assert.Equal(t, expectedMap["dmm_id"], actualMap["dmm_id"])
				assert.Equal(t, expectedMap["first_name"], actualMap["first_name"])
				assert.Equal(t, expectedMap["last_name"], actualMap["last_name"])
				assert.Equal(t, expectedMap["japanese_name"], actualMap["japanese_name"])
				assert.Equal(t, expectedMap["aliases"], actualMap["aliases"])
			}

			if tt.validateJSON != nil {
				tt.validateJSON(t, actual)
			}
		})
	}
}

// TestActressJSONUnmarshaling tests JSON to Actress struct conversion (AC-2.4.3)
func TestActressJSONUnmarshaling(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantErr  bool
		validate func(*testing.T, *Actress)
	}{
		{
			name: "unmarshal valid JSON to actress",
			json: `{
				"id": 1,
				"dmm_id": 123456,
				"first_name": "Yui",
				"last_name": "Hatano",
				"japanese_name": "波多野結衣",
				"thumb_url": "https://example.com/thumb.jpg",
				"aliases": "Alias1|Alias2",
				"created_at": "2023-01-15T10:30:00Z",
				"updated_at": "2023-01-15T10:30:00Z"
			}`,
			wantErr: false,
			validate: func(t *testing.T, a *Actress) {
				assert.Equal(t, uint(1), a.ID)
				assert.Equal(t, 123456, a.DMMID)
				assert.Equal(t, "Yui", a.FirstName)
				assert.Equal(t, "Hatano", a.LastName)
				assert.Equal(t, "波多野結衣", a.JapaneseName)
				assert.Equal(t, "Alias1|Alias2", a.Aliases)
			},
		},
		{
			name: "unmarshal JSON with missing optional fields",
			json: `{
				"japanese_name": "Test"
			}`,
			wantErr: false,
			validate: func(t *testing.T, a *Actress) {
				assert.Equal(t, uint(0), a.ID) // Zero value
				assert.Equal(t, 0, a.DMMID)    // Zero value
				assert.Equal(t, "Test", a.JapaneseName)
				assert.Empty(t, a.FirstName)
				assert.Empty(t, a.Aliases)
			},
		},
		{
			name: "unmarshal JSON with zero DMMID",
			json: `{
				"dmm_id": 0,
				"japanese_name": "No DMM ID"
			}`,
			wantErr: false,
			validate: func(t *testing.T, a *Actress) {
				assert.Equal(t, 0, a.DMMID, "Zero DMMID should unmarshal correctly")
				assert.Equal(t, "No DMM ID", a.JapaneseName)
			},
		},
		{
			name:    "unmarshal invalid JSON",
			json:    `{"invalid json syntax`,
			wantErr: true,
		},
		{
			name: "unmarshal JSON with empty aliases",
			json: `{
				"japanese_name": "Test",
				"aliases": ""
			}`,
			wantErr: false,
			validate: func(t *testing.T, a *Actress) {
				assert.Empty(t, a.Aliases, "Empty aliases should unmarshal as empty string")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var actress Actress
			err := json.Unmarshal([]byte(tt.json), &actress)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, &actress)
			}
		})
	}
}

// TestActressGORMTags validates GORM tags on Actress struct fields (AC-2.4.4)
func TestActressGORMTags(t *testing.T) {
	actressType := reflect.TypeOf(Actress{})

	tests := []struct {
		name         string
		fieldName    string
		expectedTags []string
		description  string
	}{
		{
			name:         "ID field has primaryKey tag",
			fieldName:    "ID",
			expectedTags: []string{"primaryKey"},
			description:  "Primary key for actress records",
		},
		{
			name:         "DMMID field has uniqueIndex tag",
			fieldName:    "DMMID",
			expectedTags: []string{"uniqueIndex"},
			description:  "Unique index prevents duplicate actresses from multiple scrapers",
		},
		{
			name:         "JapaneseName field has index tag",
			fieldName:    "JapaneseName",
			expectedTags: []string{"index"},
			description:  "Index for efficient name lookups",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			field, found := actressType.FieldByName(tt.fieldName)
			require.True(t, found, "Field %s should exist in Actress struct", tt.fieldName)

			gormTag := field.Tag.Get("gorm")
			for _, expectedTag := range tt.expectedTags {
				assert.Contains(t, gormTag, expectedTag,
					"Field %s should have %s tag: %s", tt.fieldName, expectedTag, tt.description)
			}
		})
	}

	// Verify timestamp fields exist (CreatedAt, UpdatedAt)
	t.Run("timestamp fields exist", func(t *testing.T) {
		t.Parallel()
		_, foundCreated := actressType.FieldByName("CreatedAt")
		_, foundUpdated := actressType.FieldByName("UpdatedAt")
		assert.True(t, foundCreated, "CreatedAt field should exist")
		assert.True(t, foundUpdated, "UpdatedAt field should exist")
	})

	t.Log("✓ GORM tags validated - ready for database integration in Epic 3")
	t.Log("✓ Full GORM integration tests (actual DB operations) deferred to Epic 3")
}

// TestActressFullNameMethod tests FullName() method with various name combinations (AC-2.4.5)
func TestActressFullNameMethod(t *testing.T) {
	tests := []struct {
		name         string
		actress      *Actress
		expectedName string
	}{
		{
			name: "LastName and FirstName present",
			actress: &Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			expectedName: "Hatano Yui",
		},
		{
			name: "FirstName only",
			actress: &Actress{
				FirstName: "Yui",
			},
			expectedName: "Yui",
		},
		{
			name: "JapaneseName only",
			actress: &Actress{
				JapaneseName: "波多野結衣",
			},
			expectedName: "波多野結衣",
		},
		{
			name: "all three names present",
			actress: &Actress{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
			// When both FirstName and LastName present, uses those
			expectedName: "Hatano Yui",
		},
		{
			name:         "empty actress",
			actress:      &Actress{},
			expectedName: "",
		},
		{
			name: "only LastName (no FirstName)",
			actress: &Actress{
				LastName:     "Hatano",
				JapaneseName: "波多野",
			},
			// Without FirstName, falls back to JapaneseName
			expectedName: "波多野",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.actress.FullName()
			assert.Equal(t, tt.expectedName, result)
		})
	}

	t.Log("✓ FullName() method tested - complements existing tests in models_test.go")
}

// TestActressTableName tests TableName() method for GORM (AC-2.4.5)
func TestActressTableName(t *testing.T) {
	actress := Actress{}
	tableName := actress.TableName()
	assert.Equal(t, "actresses", tableName, "TableName() should return 'actresses'")
}
