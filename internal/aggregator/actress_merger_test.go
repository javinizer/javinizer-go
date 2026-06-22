package aggregator

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActressMerger_SingleSource tests that a single source with one actress
// returns that actress with correct fields.
func TestActressMerger_SingleSource(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev"},
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 1)
	assert.Equal(t, "Yui", actresses[0].FirstName)
	assert.Equal(t, "Hatano", actresses[0].LastName)
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName)
}

// TestActressMerger_MultipleSourcesPriority tests that two sources with the same
// actress (different DMMID) deduplicate, keep higher-priority fields, and fill
// empty fields from lower priority.
func TestActressMerger_MultipleSourcesPriority(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
				},
			},
		},
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					JapaneseName: "波多野結衣",
					ThumbURL:     "https://example.com/thumb.jpg",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "dmm"},
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 1)
	assert.Equal(t, "Yui", actresses[0].FirstName)
	assert.Equal(t, "Hatano", actresses[0].LastName)
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName)
	assert.Equal(t, 12345, actresses[0].DMMID)
	assert.Equal(t, "https://example.com/thumb.jpg", actresses[0].ThumbURL)
}

// TestActressMerger_DMMIDDeduplicationSameIDDifferentNames tests that actresses
// with the same DMMID but different names from multiple sources are deduplicated
// into a single actress with higher-priority data.
func TestActressMerger_DMMIDDeduplicationSameIDDifferentNames(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
					ThumbURL:     "https://r18dev.example.com/thumb.jpg",
				},
			},
		},
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yuui",
					LastName:     "Hatano",
					JapaneseName: "波多野ゆい",
					ThumbURL:     "https://dmm.example.com/thumb.jpg",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "dmm"},
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 1, "Should deduplicate actresses with same DMMID")
	assert.Equal(t, 12345, actresses[0].DMMID)
	assert.Equal(t, "Yui", actresses[0].FirstName, "Should use r18dev FirstName (higher priority)")
	assert.Equal(t, "Hatano", actresses[0].LastName, "Should use r18dev LastName (higher priority)")
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName, "Should use r18dev JapaneseName (higher priority)")
	assert.Equal(t, "https://r18dev.example.com/thumb.jpg", actresses[0].ThumbURL, "Should use r18dev ThumbURL (higher priority)")
}

// TestActressMerger_DMMIDUpgradeScenario tests the DMMID upgrade scenario where
// the first source provides an actress without DMMID, the second provides the same
// actress with DMMID — the actress should be upgraded with the DMMID and fields
// from the first source preserved.
func TestActressMerger_DMMIDUpgradeScenario(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					DMMID:        0,
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
					ThumbURL:     "https://r18dev.example.com/thumb.jpg",
				},
			},
		},
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "",
					LastName:     "",
					JapaneseName: "波多野結衣",
					ThumbURL:     "",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "dmm"},
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 1, "Should merge actress and upgrade with DMMID")
	assert.Equal(t, 12345, actresses[0].DMMID, "Should upgrade actress with DMMID from dmm")
	assert.Equal(t, "Yui", actresses[0].FirstName, "Should keep r18dev FirstName")
	assert.Equal(t, "Hatano", actresses[0].LastName, "Should keep r18dev LastName")
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName, "Should keep JapaneseName")
	assert.Equal(t, "https://r18dev.example.com/thumb.jpg", actresses[0].ThumbURL, "Should keep r18dev ThumbURL")
}

// TestActressMerger_DMMIDPartialDataMerging tests that when multiple sources
// provide the same actress (by DMMID) with partial data, all fields are merged
// according to priority.
func TestActressMerger_DMMIDPartialDataMerging(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yui",
					LastName:     "",
					JapaneseName: "波多野結衣",
					ThumbURL:     "",
				},
			},
		},
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yuui",
					LastName:     "Hatano",
					JapaneseName: "波多野ゆい",
					ThumbURL:     "https://dmm.example.com/thumb.jpg",
				},
			},
		},
		{
			Source: "javlibrary",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yui H.",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
					ThumbURL:     "https://javlib.example.com/thumb.jpg",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "dmm", "javlibrary"},
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 1, "Should deduplicate actresses with same DMMID")
	assert.Equal(t, 12345, actresses[0].DMMID)
	assert.Equal(t, "Yui", actresses[0].FirstName, "Should use r18dev FirstName (highest priority)")
	assert.Equal(t, "Hatano", actresses[0].LastName, "Should use dmm LastName (r18dev had empty)")
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName, "Should use r18dev JapaneseName (highest priority)")
	assert.Equal(t, "https://dmm.example.com/thumb.jpg", actresses[0].ThumbURL, "Should use dmm ThumbURL (r18dev had empty)")
}

// TestActressMerger_DMMIDZeroNotDeduplicated tests that actresses with DMMID=0
// are merged by name (not by DMMID) since 0 is not a valid identifier.
func TestActressMerger_DMMIDZeroNotDeduplicated(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					DMMID:        0,
					FirstName:    "Unknown",
					LastName:     "Actress",
					JapaneseName: "未知の女優",
					ThumbURL:     "https://r18dev.example.com/thumb1.jpg",
				},
			},
		},
		{
			Source: "javlibrary",
			Actresses: []models.ActressInfo{
				{
					DMMID:        0,
					FirstName:    "Unknown",
					LastName:     "Actress",
					JapaneseName: "未知の女優",
					ThumbURL:     "https://javlib.example.com/thumb2.jpg",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "javlibrary"},
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 1, "Should merge actresses with same name even without DMMID")
	assert.Equal(t, 0, actresses[0].DMMID)
	assert.Equal(t, "Unknown", actresses[0].FirstName)
	assert.Equal(t, "Actress", actresses[0].LastName)
	assert.Equal(t, "未知の女優", actresses[0].JapaneseName)
	assert.Equal(t, "https://r18dev.example.com/thumb1.jpg", actresses[0].ThumbURL)
}

// TestActressMerger_UnknownActressFallback tests that when no actresses are found
// from scrapers and SkipUnknown is false with UnknownText set, a fallback actress
// is returned.
func TestActressMerger_UnknownActressFallback(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source:    "r18dev",
			Actresses: []models.ActressInfo{},
		},
	}
	opts := actressMergeOptions{
		Priority:    []string{"r18dev"},
		SkipUnknown: false,
		UnknownText: "Unknown",
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 1)
	assert.Equal(t, "Unknown", actresses[0].FirstName)
	assert.Equal(t, "Unknown", actresses[0].JapaneseName)
}

// TestActressMerger_SkipUnknown tests that an actress matching the unknown text
// is filtered out when SkipUnknown is true.
func TestActressMerger_SkipUnknown(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					FirstName: "Unknown",
					LastName:  "Actress",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority:    []string{"r18dev"},
		SkipUnknown: true,
		UnknownText: "unknown",
	}

	actresses := merger.Merge(sources, opts)

	assert.Empty(t, actresses, "Should filter out actress matching unknown text when SkipUnknown is true")
}

// TestActressMerger_NoActressesNoFallback tests that when no actresses are found
// and SkipUnknown is true, an empty slice (not nil) is returned.
func TestActressMerger_NoActressesNoFallback(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source:    "r18dev",
			Actresses: []models.ActressInfo{},
		},
	}
	opts := actressMergeOptions{
		Priority:    []string{"r18dev"},
		SkipUnknown: true,
		UnknownText: "unknown",
	}

	actresses := merger.Merge(sources, opts)

	assert.NotNil(t, actresses, "Should return non-nil empty slice")
	assert.Empty(t, actresses, "Should return empty slice when no actresses and SkipUnknown is true")
}

// TestActressMerger_NilAliasResolver tests that a nil AliasResolver in opts
// does not cause a panic and actresses are returned unchanged.
func TestActressMerger_NilAliasResolver(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority:      []string{"r18dev"},
		AliasResolver: nil,
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 1)
	assert.Equal(t, "Yui", actresses[0].FirstName)
	assert.Equal(t, "Hatano", actresses[0].LastName)
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName)
}

// TestActressMerger_AliasResolution tests that when an AliasResolver is provided,
// actresses are resolved through it.
func TestActressMerger_AliasResolution(t *testing.T) {
	merger := newActressMerger()

	resolver := newAliasResolverWithCache(
		&MetadataConfig{
			ActressDatabase: actressDatabaseConfigView{
				Enabled:      true,
				ConvertAlias: true,
			},
		},
		nil,
		map[string]string{
			"Yui Hatano": "Hatano Yui",
			"波多野結衣":      "はたのゆい",
		},
	)

	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority:      []string{"r18dev"},
		AliasResolver: resolver,
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 1)
	// After alias resolution: "Yui Hatano" -> "Hatano Yui"
	assert.Equal(t, "Hatano Yui", actresses[0].FullName())
}

// TestActressMerger_JapaneseNameVsFirstNameMerge tests that DMMID in one source
// with JapaneseName and FirstName in another source with a matching name key
// results in a single merged actress.
func TestActressMerger_JapaneseNameVsFirstNameMerge(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        1044046,
					JapaneseName: "河合あすな",
					ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/kawai_asuna.jpg",
				},
			},
		},
		{
			Source: "javlibrary",
			Actresses: []models.ActressInfo{
				{
					FirstName: "河合あすな",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"dmm", "javlibrary"},
	}

	actresses := merger.Merge(sources, opts)

	require.Len(t, actresses, 1, "should merge Japanese-name-in-FirstName with JapaneseName")
	assert.Equal(t, "河合あすな", actresses[0].JapaneseName)
	assert.Equal(t, 1044046, actresses[0].DMMID)
	assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/kawai_asuna.jpg", actresses[0].ThumbURL)
}
