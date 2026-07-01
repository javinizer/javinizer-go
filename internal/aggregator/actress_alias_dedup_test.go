package aggregator

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActressAliasCrossSourceDedup reproduces the DOCP-392 scenario where
// libredmm credits an actress under her current name while JavDB credits the
// same person under a historical alias. Before the fix, the merger produced 7
// actresses (3 duplicates) because Phase 1 dedup ran on raw names before alias
// resolution, and Phase 2 only renamed — never merged. The alias DB is now
// consulted for the dedup key itself, so alias credits collapse into the
// canonical entry.
func TestActressAliasCrossSourceDedup(t *testing.T) {
	resolver := newAliasResolverWithCache(&MetadataConfig{
		ActressDatabase: actressDatabaseConfigView{
			Enabled:      true,
			ConvertAlias: false, // display rename OFF — dedup must still work
		},
	}, nil, map[string]string{
		"青木桃":   "新セリナ",
		"朝日芹奈":  "新セリナ",
		"堤セリナ":  "新セリナ",
		"与田さくら": "尾崎えりか",
		"広瀬みつき": "日向ゆら",
	})

	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "libredmm",
			Actresses: []models.ActressInfo{
				{JapaneseName: "尾崎えりか"},
				{JapaneseName: "弥生みづき"},
				{JapaneseName: "朝日芹奈"},
				{JapaneseName: "日向ゆら"},
			},
		},
		{
			Source: "javdb",
			Actresses: []models.ActressInfo{
				{JapaneseName: "与田さくら"},
				{JapaneseName: "広瀬みつき"},
				{JapaneseName: "青木桃"},
			},
		},
	}
	opts := actressMergeOptions{
		Priority:      []string{"libredmm", "javdb"},
		AliasResolver: resolver,
	}

	actresses := merger.Merge(sources, opts)

	t.Logf("got %d actresses:", len(actresses))
	for i, a := range actresses {
		t.Logf("  [%d] ja=%q", i+1, a.JapaneseName)
	}

	// Real cast is 4 people; JavDB's 3 names are historical aliases of 3 of
	// them, so the merged result must be 4 — not 7.
	require.Len(t, actresses, 4, "alias credits should merge into their canonical entries")

	// With ConvertAlias=false the display name is the higher-priority source's
	// raw name (libredmm's release-time credit), not the canonical.
	names := make(map[string]bool, len(actresses))
	for _, a := range actresses {
		names[a.JapaneseName] = true
	}
	assert.True(t, names["尾崎えりか"])
	assert.True(t, names["弥生みづき"])
	assert.True(t, names["朝日芹奈"])
	assert.True(t, names["日向ゆら"])
	// The alias names must NOT survive as separate entries.
	assert.False(t, names["与田さくら"])
	assert.False(t, names["広瀬みつき"])
	assert.False(t, names["青木桃"])
}

// TestActressAliasCrossSourceDedup_WithConvertAlias confirms that when
// ConvertAlias is also enabled, the merged entries are renamed to the
// canonical form — matching community wikis that track current stage names.
func TestActressAliasCrossSourceDedup_WithConvertAlias(t *testing.T) {
	resolver := newAliasResolverWithCache(&MetadataConfig{
		ActressDatabase: actressDatabaseConfigView{
			Enabled:      true,
			ConvertAlias: true,
		},
	}, nil, map[string]string{
		"青木桃":   "新セリナ",
		"朝日芹奈":  "新セリナ",
		"与田さくら": "尾崎えりか",
		"広瀬みつき": "日向ゆら",
	})

	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "libredmm",
			Actresses: []models.ActressInfo{
				{JapaneseName: "尾崎えりか"},
				{JapaneseName: "朝日芹奈"},
			},
		},
		{
			Source: "javdb",
			Actresses: []models.ActressInfo{
				{JapaneseName: "与田さくら"},
				{JapaneseName: "青木桃"},
			},
		},
	}
	opts := actressMergeOptions{
		Priority:      []string{"libredmm", "javdb"},
		AliasResolver: resolver,
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 2, "aliases must dedup to the canonical entries")

	names := make(map[string]bool, len(actresses))
	for _, a := range actresses {
		names[a.JapaneseName] = true
	}
	assert.True(t, names["尾崎えりか"], "与田さくら should canonicalize to 尾崎えりか")
	assert.True(t, names["新セリナ"], "朝日芹奈/青木桃 should canonicalize to 新セリナ")
}

// TestActressAliasDedup_NoResolver ensures that when no alias resolver is
// wired (e.g. actress_database disabled), the merger falls back to the
// original raw-name dedup behavior — no regression for the no-DB path.
func TestActressAliasDedup_NoResolver(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "libredmm",
			Actresses: []models.ActressInfo{
				{JapaneseName: "朝日芹奈"},
			},
		},
		{
			Source: "javdb",
			Actresses: []models.ActressInfo{
				{JapaneseName: "青木桃"}, // different raw name, no alias DB
			},
		},
	}
	opts := actressMergeOptions{
		Priority:      []string{"libredmm", "javdb"},
		AliasResolver: nil,
	}

	actresses := merger.Merge(sources, opts)

	// Without alias data the merger cannot know these are the same person, so
	// both survive — preserving the pre-fix behavior for the no-DB path.
	require.Len(t, actresses, 2)
}
