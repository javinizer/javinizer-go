package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergeFieldDecisionV5 tests merge field decision validation
func TestMergeFieldDecisionV5(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"", "target", false},
		{"target", "target", false},
		{"source", "source", false},
		{"TARGET", "target", false},
		{"SOURCE", "source", false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := mergeFieldDecision(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestNormalizeMergeResolutionsV5 tests merge resolution normalization
func TestNormalizeMergeResolutionsV5(t *testing.T) {
	t.Run("valid resolutions", func(t *testing.T) {
		input := map[string]string{
			"dmm_id":        "source",
			"first_name":    "target",
			"japanese_name": "source",
		}
		result, err := normalizeMergeResolutions(input)
		require.NoError(t, err)
		assert.Equal(t, "source", result["dmm_id"])
		assert.Equal(t, "target", result["first_name"])
	})

	t.Run("invalid field", func(t *testing.T) {
		input := map[string]string{
			"invalid_field": "target",
		}
		_, err := normalizeMergeResolutions(input)
		assert.Error(t, err)
	})

	t.Run("invalid decision", func(t *testing.T) {
		input := map[string]string{
			"dmm_id": "invalid",
		}
		_, err := normalizeMergeResolutions(input)
		assert.Error(t, err)
	})
}

// TestBuildActressMergeConflictsV5 tests conflict detection
func TestBuildActressMergeConflictsV5(t *testing.T) {
	t.Run("dmm_id conflict", func(t *testing.T) {
		target := &models.Actress{DMMID: 1}
		source := &models.Actress{DMMID: 2}
		conflicts := buildActressMergeConflicts(target, source)
		assert.Len(t, conflicts, 1)
		assert.Equal(t, "dmm_id", conflicts[0].Field)
	})

	t.Run("name conflicts", func(t *testing.T) {
		target := &models.Actress{FirstName: "John", LastName: "Doe", JapaneseName: "日本名", ThumbURL: "http://a.jpg"}
		source := &models.Actress{FirstName: "Jane", LastName: "Smith", JapaneseName: "別名", ThumbURL: "http://b.jpg"}
		conflicts := buildActressMergeConflicts(target, source)
		assert.GreaterOrEqual(t, len(conflicts), 4)
	})

	t.Run("no conflicts", func(t *testing.T) {
		target := &models.Actress{FirstName: "Same"}
		source := &models.Actress{FirstName: "Same"}
		conflicts := buildActressMergeConflicts(target, source)
		assert.Len(t, conflicts, 0)
	})
}

// TestDefaultResolutionsFromConflictsV5 tests default resolution creation
func TestDefaultResolutionsFromConflictsV5(t *testing.T) {
	conflicts := []ActressMergeConflict{
		{Field: "dmm_id", DefaultResolution: "target"},
		{Field: "first_name", DefaultResolution: "target"},
	}
	result := defaultResolutionsFromConflicts(conflicts)
	assert.Len(t, result, 2)
	assert.Equal(t, "target", result["dmm_id"])
	assert.Equal(t, "target", result["first_name"])
}

// TestCanonicalActressNameV5 tests canonical name selection
func TestCanonicalActressNameV5(t *testing.T) {
	tests := []struct {
		name     string
		actress  *models.Actress
		expected string
	}{
		{"JapaneseName preferred", &models.Actress{JapaneseName: "日本名", FirstName: "John"}, "日本名"},
		{"FullName fallback", &models.Actress{FirstName: "John", LastName: "Doe"}, "Doe John"},
		{"FirstName only", &models.Actress{FirstName: "John"}, "John"},
		{"LastName only", &models.Actress{LastName: "Doe"}, "Doe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalActressName(tt.actress)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSplitAliasListV5 tests alias list splitting
func TestSplitAliasListV5(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"alias1|alias2|alias3", []string{"alias1", "alias2", "alias3"}},
		{"alias1", []string{"alias1"}},
		{"", []string{}},
		{"alias1||alias2", []string{"alias1", "alias2"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := splitAliasList(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCollectActressAliasCandidatesV5 tests alias candidate collection
func TestCollectActressAliasCandidatesV5(t *testing.T) {
	t.Run("with JapaneseName", func(t *testing.T) {
		a := &models.Actress{JapaneseName: "日本名", FirstName: "John", LastName: "Doe"}
		candidates := collectActressAliasCandidates(a)
		assert.Contains(t, candidates, "日本名")
	})

	t.Run("with names", func(t *testing.T) {
		a := &models.Actress{FirstName: "John", LastName: "Doe"}
		candidates := collectActressAliasCandidates(a)
		assert.Contains(t, candidates, "Doe John")
		assert.Contains(t, candidates, "John Doe")
	})
}

// TestMergeAliasValuesV5 tests alias merging
func TestMergeAliasValuesV5(t *testing.T) {
	t.Run("merge new aliases", func(t *testing.T) {
		merged, count, added := mergeAliasValues("alias1", []string{"alias2", "alias3"}, "Canonical")
		assert.Contains(t, merged, "alias1")
		assert.Contains(t, merged, "alias2")
		assert.Contains(t, merged, "alias3")
		assert.Equal(t, 2, count)
		assert.Len(t, added, 2)
	})

	t.Run("dedup aliases", func(t *testing.T) {
		merged, count, _ := mergeAliasValues("alias1", []string{"alias1", "alias2"}, "Canonical")
		assert.Equal(t, 1, count) // alias1 already present
		assert.Contains(t, merged, "alias2")
	})

	t.Run("skip canonical name", func(t *testing.T) {
		_, count, _ := mergeAliasValues("alias1", []string{"CanonicalName"}, "CanonicalName")
		assert.Equal(t, 0, count)
	})
}

// TestSourceAliasesForUpsertV5 tests source alias filtering for upsert
func TestSourceAliasesForUpsertV5(t *testing.T) {
	result := sourceAliasesForUpsert([]string{"alias1", "alias2", "Canonical"}, "Canonical")
	assert.Len(t, result, 2)
	assert.Contains(t, result, "alias1")
	assert.Contains(t, result, "alias2")
}

// TestNonEmptyStringV5 tests non-empty string check
func TestNonEmptyStringV5(t *testing.T) {
	assert.True(t, nonEmptyString("hello"))
	assert.False(t, nonEmptyString(""))
	assert.False(t, nonEmptyString("   "))
}

// TestMergeActressValuesV5 tests actress value merging
func TestMergeActressValuesV5(t *testing.T) {
	t.Run("target wins by default", func(t *testing.T) {
		target := &models.Actress{DMMID: 1, FirstName: "Target"}
		source := &models.Actress{DMMID: 2, FirstName: "Source"}
		merged, err := mergeActressValues(target, source, map[string]string{"dmm_id": "target", "first_name": "target"})
		require.NoError(t, err)
		assert.Equal(t, 1, merged.DMMID)
		assert.Equal(t, "Target", merged.FirstName)
	})

	t.Run("source wins when specified", func(t *testing.T) {
		target := &models.Actress{DMMID: 1, FirstName: "Target"}
		source := &models.Actress{DMMID: 2, FirstName: "Source"}
		merged, err := mergeActressValues(target, source, map[string]string{"dmm_id": "source", "first_name": "source"})
		require.NoError(t, err)
		assert.Equal(t, 2, merged.DMMID)
		assert.Equal(t, "Source", merged.FirstName)
	})

	t.Run("source fills empty fields", func(t *testing.T) {
		target := &models.Actress{DMMID: 0}
		source := &models.Actress{DMMID: 5}
		merged, err := mergeActressValues(target, source, map[string]string{})
		require.NoError(t, err)
		assert.Equal(t, 5, merged.DMMID)
	})
}

// TestAppendConflictV5 tests conflict appending
func TestAppendConflictV5(t *testing.T) {
	conflicts := make([]ActressMergeConflict, 0)
	conflicts = appendConflict(conflicts, "field1", "value1", "value2")
	assert.Len(t, conflicts, 1)
	assert.Equal(t, "field1", conflicts[0].Field)
	assert.Equal(t, "target", conflicts[0].DefaultResolution)
}
