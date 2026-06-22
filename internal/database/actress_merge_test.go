package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestMergeFieldDecision(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		hasError bool
	}{
		{"target", "target", false},
		{"source", "source", false},
		{"", "target", false},
		{"  ", "target", false},
		{"TARGET", "target", false},
		{"Source", "source", false},
		{"invalid", "", true},
		{"both", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := mergeFieldDecision(tt.input)
			if tt.hasError {
				assert.Error(t, err)
				assert.True(t, err == ErrActressMergeInvalidDecision || err.Error() != "")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestNormalizeMergeResolutions(t *testing.T) {
	t.Run("valid fields", func(t *testing.T) {
		input := map[string]string{
			"dmm_id":        "source",
			"first_name":    "target",
			"last_name":     "source",
			"japanese_name": "target",
			"thumb_url":     "source",
		}
		result, err := normalizeMergeResolutions(input)
		assert.NoError(t, err)
		assert.Equal(t, "source", result["dmm_id"])
		assert.Equal(t, "target", result["first_name"])
	})

	t.Run("invalid field name", func(t *testing.T) {
		input := map[string]string{
			"invalid_field": "source",
		}
		_, err := normalizeMergeResolutions(input)
		assert.Error(t, err)
	})

	t.Run("invalid decision value", func(t *testing.T) {
		input := map[string]string{
			"dmm_id": "invalid",
		}
		_, err := normalizeMergeResolutions(input)
		assert.Error(t, err)
	})

	t.Run("whitespace trimming", func(t *testing.T) {
		input := map[string]string{
			"  dmm_id  ": "  source  ",
		}
		result, err := normalizeMergeResolutions(input)
		assert.NoError(t, err)
		assert.Equal(t, "source", result["dmm_id"])
	})

	t.Run("empty map", func(t *testing.T) {
		result, err := normalizeMergeResolutions(map[string]string{})
		assert.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestCanonicalActressName(t *testing.T) {
	tests := []struct {
		name     string
		actress  models.Actress
		expected string
	}{
		{
			name:     "JapaneseName takes priority",
			actress:  models.Actress{JapaneseName: "日本名", FirstName: "First", LastName: "Last"},
			expected: "日本名",
		},
		{
			name:     "FullName when no JapaneseName",
			actress:  models.Actress{FirstName: "First", LastName: "Last"},
			expected: "Last First",
		},
		{
			name:     "FirstName when no LastName or JapaneseName",
			actress:  models.Actress{FirstName: "OnlyFirst"},
			expected: "OnlyFirst",
		},
		{
			name:     "LastName when only LastName",
			actress:  models.Actress{LastName: "OnlyLast"},
			expected: "OnlyLast",
		},
		{
			name:     "empty actress returns empty",
			actress:  models.Actress{},
			expected: "",
		},
		{
			name:     "whitespace-only JapaneseName falls back",
			actress:  models.Actress{JapaneseName: "  ", FirstName: "FallBack", LastName: "Name"},
			expected: "Name FallBack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalActressName(&tt.actress)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollectActressAliasCandidates(t *testing.T) {
	t.Run("with all fields", func(t *testing.T) {
		actress := &models.Actress{
			JapaneseName: "日本名",
			FirstName:    "First",
			LastName:     "Last",
			Aliases:      "Alias1|Alias2",
		}
		candidates := collectActressAliasCandidates(actress)
		assert.Contains(t, candidates, "Alias1")
		assert.Contains(t, candidates, "Alias2")
		assert.Contains(t, candidates, "日本名")
		assert.Contains(t, candidates, "Last First")
		assert.Contains(t, candidates, "First Last")
	})

	t.Run("with only FirstName", func(t *testing.T) {
		actress := &models.Actress{FirstName: "OnlyFirst"}
		candidates := collectActressAliasCandidates(actress)
		assert.Contains(t, candidates, "OnlyFirst")
	})

	t.Run("with only LastName", func(t *testing.T) {
		actress := &models.Actress{LastName: "OnlyLast"}
		candidates := collectActressAliasCandidates(actress)
		assert.Contains(t, candidates, "OnlyLast")
	})

	t.Run("empty actress", func(t *testing.T) {
		actress := &models.Actress{}
		candidates := collectActressAliasCandidates(actress)
		assert.Empty(t, candidates)
	})
}

func TestMergeAliasValues(t *testing.T) {
	t.Run("merges with deduplication", func(t *testing.T) {
		merged, count, added := mergeAliasValues("Alias1|Alias2", []string{"Alias2", "Alias3"}, "Canonical")
		assert.Contains(t, merged, "Alias1")
		assert.Contains(t, merged, "Alias2")
		assert.Contains(t, merged, "Alias3")
		assert.Equal(t, 1, count)
		assert.Len(t, added, 1)
		assert.Contains(t, added, "Alias3")
	})

	t.Run("excludes canonical name", func(t *testing.T) {
		merged, count, _ := mergeAliasValues("Alias1", []string{"Canonical", "Alias2"}, "Canonical")
		assert.NotContains(t, merged, "Canonical")
		assert.Contains(t, merged, "Alias1")
		assert.Contains(t, merged, "Alias2")
		assert.Equal(t, 1, count)
	})

	t.Run("empty target aliases", func(t *testing.T) {
		merged, count, added := mergeAliasValues("", []string{"NewAlias"}, "Canonical")
		assert.Contains(t, merged, "NewAlias")
		assert.Equal(t, 1, count)
		assert.Len(t, added, 1)
	})

	t.Run("empty source candidates", func(t *testing.T) {
		merged, count, added := mergeAliasValues("Existing", nil, "Canonical")
		assert.Contains(t, merged, "Existing")
		assert.Equal(t, 0, count)
		assert.Empty(t, added)
	})
}

func TestMergeActressValues(t *testing.T) {
	t.Run("source DMMID when target has none", func(t *testing.T) {
		target := &models.Actress{FirstName: "T", LastName: "T"}
		source := &models.Actress{DMMID: 99999, FirstName: "S", LastName: "S"}
		merged, err := mergeActressValues(target, source, map[string]string{})
		assert.NoError(t, err)
		assert.Equal(t, 99999, merged.DMMID)
	})

	t.Run("source FirstName when target empty", func(t *testing.T) {
		target := &models.Actress{LastName: "Last"}
		source := &models.Actress{FirstName: "SourceFirst", LastName: "Last"}
		merged, err := mergeActressValues(target, source, map[string]string{})
		assert.NoError(t, err)
		assert.Equal(t, "SourceFirst", merged.FirstName)
	})

	t.Run("source LastName when target empty", func(t *testing.T) {
		target := &models.Actress{FirstName: "First"}
		source := &models.Actress{FirstName: "First", LastName: "SourceLast"}
		merged, err := mergeActressValues(target, source, map[string]string{})
		assert.NoError(t, err)
		assert.Equal(t, "SourceLast", merged.LastName)
	})

	t.Run("source JapaneseName when target empty", func(t *testing.T) {
		target := &models.Actress{}
		source := &models.Actress{JapaneseName: "ソース名"}
		merged, err := mergeActressValues(target, source, map[string]string{})
		assert.NoError(t, err)
		assert.Equal(t, "ソース名", merged.JapaneseName)
	})

	t.Run("source ThumbURL when target empty", func(t *testing.T) {
		target := &models.Actress{}
		source := &models.Actress{ThumbURL: "http://example.com/thumb.jpg"}
		merged, err := mergeActressValues(target, source, map[string]string{})
		assert.NoError(t, err)
		assert.Equal(t, "http://example.com/thumb.jpg", merged.ThumbURL)
	})

	t.Run("conflicting DMMID resolved to source", func(t *testing.T) {
		target := &models.Actress{DMMID: 1}
		source := &models.Actress{DMMID: 2}
		merged, err := mergeActressValues(target, source, map[string]string{"dmm_id": "source"})
		assert.NoError(t, err)
		assert.Equal(t, 2, merged.DMMID)
	})

	t.Run("conflicting FirstName resolved to source", func(t *testing.T) {
		target := &models.Actress{FirstName: "TargetFirst"}
		source := &models.Actress{FirstName: "SourceFirst"}
		merged, err := mergeActressValues(target, source, map[string]string{"first_name": "source"})
		assert.NoError(t, err)
		assert.Equal(t, "SourceFirst", merged.FirstName)
	})

	t.Run("conflicting LastName resolved to source", func(t *testing.T) {
		target := &models.Actress{LastName: "TargetLast"}
		source := &models.Actress{LastName: "SourceLast"}
		merged, err := mergeActressValues(target, source, map[string]string{"last_name": "source"})
		assert.NoError(t, err)
		assert.Equal(t, "SourceLast", merged.LastName)
	})

	t.Run("conflicting JapaneseName resolved to source", func(t *testing.T) {
		target := &models.Actress{JapaneseName: "ターゲット"}
		source := &models.Actress{JapaneseName: "ソース"}
		merged, err := mergeActressValues(target, source, map[string]string{"japanese_name": "source"})
		assert.NoError(t, err)
		assert.Equal(t, "ソース", merged.JapaneseName)
	})

	t.Run("conflicting ThumbURL resolved to source", func(t *testing.T) {
		target := &models.Actress{ThumbURL: "http://target.jpg"}
		source := &models.Actress{ThumbURL: "http://source.jpg"}
		merged, err := mergeActressValues(target, source, map[string]string{"thumb_url": "source"})
		assert.NoError(t, err)
		assert.Equal(t, "http://source.jpg", merged.ThumbURL)
	})

	t.Run("invalid resolution decision returns error", func(t *testing.T) {
		target := &models.Actress{DMMID: 1}
		source := &models.Actress{DMMID: 2}
		_, err := mergeActressValues(target, source, map[string]string{"dmm_id": "invalid"})
		assert.Error(t, err)
	})
}

func TestSplitAliasList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"simple", "a|b|c", []string{"a", "b", "c"}},
		{"with whitespace", " a | b | c ", []string{"a", "b", "c"}},
		{"empty parts", "a||b", []string{"a", "b"}},
		{"single", "alias", []string{"alias"}},
		{"empty string", "", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitAliasList(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSourceAliasesForUpsert(t *testing.T) {
	t.Run("filters canonical name and deduplicates", func(t *testing.T) {
		result := sourceAliasesForUpsert(
			[]string{"Canonical", "Alias1", "Alias1", "Alias2"},
			"Canonical",
		)
		assert.Contains(t, result, "Alias1")
		assert.Contains(t, result, "Alias2")
		assert.NotContains(t, result, "Canonical")
		// Dedup: Alias1 should appear only once
		count := 0
		for _, a := range result {
			if a == "Alias1" {
				count++
			}
		}
		assert.Equal(t, 1, count)
	})

	t.Run("empty input", func(t *testing.T) {
		result := sourceAliasesForUpsert(nil, "Canonical")
		assert.Empty(t, result)
	})
}

func TestNonEmptyString(t *testing.T) {
	assert.True(t, nonEmptyString("hello"))
	assert.True(t, nonEmptyString("  hello  "))
	assert.False(t, nonEmptyString(""))
	assert.False(t, nonEmptyString("   "))
}
