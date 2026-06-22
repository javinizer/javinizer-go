package nfo

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests targeting uncovered lines in mergeActressSlices (merger.go:486).
// Uncovered areas:
//   - findNFOMatch JP name range loop (L573-576)
//   - findNFOMatch romanized name range loop (L585-588)
//   - Phase 2 reverse match merge body (L653-683)

// --- findNFOMatch: JP name range loop (L573-576) ---
// When multiple scraped actresses share the same JP name, the second one
// enters Phase 2 and findNFOMatch looks up the JP name in nfoIndicesByJpName.
// The range loop iterates but the NFO entry is already matched from Phase 1.

func TestMergeActressSlices_Phase2_DuplicateJPName_EntersRangeLoop(t *testing.T) {
	// scraped[0] and scraped[1] both have JP "X". NFO[0] also has JP "X".
	// Phase 1: NFO[0] matches scraped[0] by JP name.
	// Phase 2: scraped[1] (unmatched) calls findNFOMatch with JP "X".
	//   The key "jp:x" exists in nfoIndicesByJpName, so the range loop at L573 executes.
	//   But NFO[0] is already matched, so findNFOMatch returns -1.
	scraped := []models.Actress{
		{JapaneseName: "田中", FirstName: "First1", LastName: "Last1", DMMID: 10},
		{JapaneseName: "田中", FirstName: "First2", LastName: "Last2", DMMID: 20},
	}
	nfo := []models.Actress{
		{JapaneseName: "田中", FirstName: "NFOFirst", LastName: "NFOLast", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	// scraped[0] matched with NFO[0] → merged. scraped[1] unmatched → appears separately.
	assert.Len(t, result, 2)

	// Find the merged entry (scraped[0] + NFO[0])
	var merged, unmatched models.Actress
	for _, a := range result {
		if a.DMMID == 10 {
			merged = a
		} else if a.DMMID == 20 {
			unmatched = a
		}
	}
	// Merged entry should have ThumbURL from NFO
	assert.Equal(t, "http://thumb.jpg", merged.ThumbURL)
	// Unmatched scraped[1] should retain its original data
	assert.Equal(t, "First2", unmatched.FirstName)
	assert.Equal(t, 20, unmatched.DMMID)
}

// --- findNFOMatch: romanized name range loop (L585-588) ---

func TestMergeActressSlices_Phase2_DuplicateRomanizedName_EntersRangeLoop(t *testing.T) {
	// scraped[0] and scraped[1] both have romanized "A B". NFO[0] also has romanized "A B".
	// Phase 1: NFO[0] matches scraped[0] by romanized name.
	// Phase 2: scraped[1] (unmatched) calls findNFOMatch with romanized "A B".
	//   The key "name:a|b" exists in nfoIndicesByRomanizedName, so range loop at L585 executes.
	//   But NFO[0] is already matched, so findNFOMatch returns -1.
	scraped := []models.Actress{
		{FirstName: "A", LastName: "B", DMMID: 10},
		{FirstName: "A", LastName: "B", DMMID: 20},
	}
	nfo := []models.Actress{
		{FirstName: "A", LastName: "B", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2)

	var merged, unmatched models.Actress
	for _, a := range result {
		if a.DMMID == 10 {
			merged = a
		} else if a.DMMID == 20 {
			unmatched = a
		}
	}
	assert.Equal(t, "http://thumb.jpg", merged.ThumbURL)
	assert.Equal(t, "", unmatched.ThumbURL)
}

// --- findNFOMatch: JP name AND romanized name range loops ---

func TestMergeActressSlices_Phase2_DuplicateJPAndRomanized_EntersBothRangeLoops(t *testing.T) {
	// scraped has two actresses with same JP name AND same romanized name.
	// NFO has one actress with same JP and romanized.
	// Phase 2 for the second scraped actress enters both JP and romanized range loops.
	scraped := []models.Actress{
		{JapaneseName: "佐藤", FirstName: "Sato", LastName: "Hanako", DMMID: 10},
		{JapaneseName: "佐藤", FirstName: "Sato", LastName: "Hanako", DMMID: 20},
	}
	nfo := []models.Actress{
		{JapaneseName: "佐藤", FirstName: "Sato", LastName: "Hanako", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2)
}

// --- Actress with only FirstName (no LastName) ---

func TestMergeActressSlices_OnlyFirstName_RomanizedIndex(t *testing.T) {
	// An actress with only FirstName should still be indexed in romanized name index
	// (the condition is `fn != "" || ln != ""`)
	scraped := []models.Actress{
		{FirstName: "Yui"},
	}
	nfo := []models.Actress{
		{FirstName: "Yui", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, "http://thumb.jpg", result[0].ThumbURL)
}

// --- Actress with only LastName (no FirstName) ---

func TestMergeActressSlices_OnlyLastName_RomanizedIndex(t *testing.T) {
	// An actress with only LastName should still be indexed in romanized name index
	scraped := []models.Actress{
		{LastName: "Hatano"},
	}
	nfo := []models.Actress{
		{LastName: "Hatano", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, "http://thumb.jpg", result[0].ThumbURL)
}

// --- Phase 2 with preferNFO=true and unmatched scraped actress ---

func TestMergeActressSlices_Phase2_UnmatchedScrapedWithJPName_NoMatch(t *testing.T) {
	// Scraped has JP name that doesn't match any NFO actress's JP name.
	// Phase 2 findNFOMatch returns -1 (JP name key not in nfoIndicesByJpName).
	scraped := []models.Actress{
		{JapaneseName: "山田", DMMID: 50, ThumbURL: "http://sc.jpg"},
	}
	nfo := []models.Actress{
		{JapaneseName: "鈴木", FirstName: "Suzuki"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	assert.Len(t, result, 2)
}

// --- DMMID preservation from scraped source ---

func TestMergeActressSlices_DMMIDAlwaysFromScraped(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 999, ThumbURL: "http://sc.jpg"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", DMMID: 1, FirstName: "NFOFirst"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	require.Len(t, result, 1)
	assert.Equal(t, 999, result[0].DMMID, "DMMID should always come from scraped source")
}

// --- Whitespace handling in JapaneseName matching ---

func TestMergeActressSlices_JPNameWhitespaceTrimming(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "  田中  ", DMMID: 10},
	}
	nfo := []models.Actress{
		{JapaneseName: "田中", FirstName: "Tanaka", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, 10, result[0].DMMID)
	assert.Equal(t, "Tanaka", result[0].FirstName)
}

// --- Whitespace handling in romanized name matching ---

func TestMergeActressSlices_RomanizedNameWhitespaceTrimming(t *testing.T) {
	scraped := []models.Actress{
		{FirstName: "  Yui  ", LastName: "  Hatano  ", DMMID: 10},
	}
	nfo := []models.Actress{
		{FirstName: "Yui", LastName: "Hatano", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, 10, result[0].DMMID)
	assert.Equal(t, "http://thumb.jpg", result[0].ThumbURL)
}

// --- Case-insensitive JP name matching ---

func TestMergeActressSlices_JPNameCaseInsensitive(t *testing.T) {
	// Although Japanese names don't have case, this tests the ToLower logic
	// for any latin characters that might appear in JapaneseName field
	scraped := []models.Actress{
		{JapaneseName: "TestActress", DMMID: 10},
	}
	nfo := []models.Actress{
		{JapaneseName: "testactress", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, 10, result[0].DMMID)
	assert.Equal(t, "http://thumb.jpg", result[0].ThumbURL)
}

// --- Multiple NFO actresses, one matches by JP name, another by romanized ---

func TestMergeActressSlices_MultipleNFO_DifferentMatchTypes(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "田中", FirstName: "A", LastName: "B", DMMID: 10},
		{JapaneseName: "佐藤", FirstName: "C", LastName: "D", DMMID: 20},
	}
	nfo := []models.Actress{
		{JapaneseName: "田中", ThumbURL: "http://t1.jpg"},
		{FirstName: "C", LastName: "D", ThumbURL: "http://t2.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2)

	for _, a := range result {
		if a.DMMID == 10 {
			assert.Equal(t, "http://t1.jpg", a.ThumbURL)
		} else if a.DMMID == 20 {
			assert.Equal(t, "http://t2.jpg", a.ThumbURL)
		}
	}
}

// --- Phase 2: unmatched scraped actress with JP name that matches an already-matched NFO actress ---
// This exercises findNFOMatch's JP name range loop (L573) where entries are iterated but matched.

func TestMergeActressSlices_Phase2_JPNameLookupWithMatchedNFO(t *testing.T) {
	// Three scraped actresses with JP "X", one NFO actress with JP "X".
	// Phase 1: NFO[0] matches scraped[0].
	// Phase 2: scraped[1] and scraped[2] both call findNFOMatch with JP "X".
	//   The range loop at L573 iterates over nfoIndicesByJpName["jp:x"] entries.
	//   All are matched, so findNFOMatch returns -1.
	scraped := []models.Actress{
		{JapaneseName: "共通", FirstName: "A1", DMMID: 1},
		{JapaneseName: "共通", FirstName: "A2", DMMID: 2},
		{JapaneseName: "共通", FirstName: "A3", DMMID: 3},
	}
	nfo := []models.Actress{
		{JapaneseName: "共通", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 3)

	// Exactly one should have the NFO thumb
	thumbCount := 0
	for _, a := range result {
		if a.ThumbURL == "http://thumb.jpg" {
			thumbCount++
		}
	}
	assert.Equal(t, 1, thumbCount)
}

// --- Phase 2: unmatched scraped actress with romanized name that matches an already-matched NFO actress ---

func TestMergeActressSlices_Phase2_RomanizedNameLookupWithMatchedNFO(t *testing.T) {
	// Three scraped actresses with romanized "A B", one NFO actress with romanized "A B".
	// Phase 1: NFO[0] matches scraped[0] by romanized name.
	// Phase 2: scraped[1] and scraped[2] call findNFOMatch with romanized "A B".
	//   The range loop at L585 iterates over nfoIndicesByRomanizedName entries.
	scraped := []models.Actress{
		{FirstName: "A", LastName: "B", DMMID: 1},
		{FirstName: "A", LastName: "B", DMMID: 2},
		{FirstName: "A", LastName: "B", DMMID: 3},
	}
	nfo := []models.Actress{
		{FirstName: "A", LastName: "B", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 3)

	thumbCount := 0
	for _, a := range result {
		if a.ThumbURL == "http://thumb.jpg" {
			thumbCount++
		}
	}
	assert.Equal(t, 1, thumbCount)
}

// --- Phase 2: unmatched scraped with BOTH JP name and romanized name keys existing in NFO indices ---

func TestMergeActressSlices_Phase2_BothJPAndRomanizedKeysExistInNFOIndices(t *testing.T) {
	// Two scraped actresses with same JP name and romanized name.
	// One NFO actress with same JP name and romanized name.
	// Phase 2: second scraped actress tries findNFOMatch with both JP and romanized keys.
	// Both keys exist in NFO indices, but the NFO entry is already matched.
	scraped := []models.Actress{
		{JapaneseName: "共通", FirstName: "A", LastName: "B", DMMID: 1, ThumbURL: ""},
		{JapaneseName: "共通", FirstName: "A", LastName: "B", DMMID: 2, ThumbURL: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "共通", FirstName: "A", LastName: "B", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2)

	// First scraped gets merged with NFO
	thumbCount := 0
	for _, a := range result {
		if a.ThumbURL == "http://thumb.jpg" {
			thumbCount++
		}
	}
	assert.Equal(t, 1, thumbCount)
}

// --- Phase 2: unmatched scraped with romanized name matching NFO via reversed order ---

func TestMergeActressSlices_Phase2_ReversedRomanizedLookupInNFOIndices(t *testing.T) {
	// Scraped has romanized name "A B", NFO has romanized "B A" (reversed).
	// Phase 1: NFO looks up "name:b|a" and "name:a|b" in scraped indices → found.
	// Phase 2 not needed, but let's verify correct matching.
	scraped := []models.Actress{
		{FirstName: "Hanako", LastName: "Sato", DMMID: 10},
	}
	nfo := []models.Actress{
		{FirstName: "Sato", LastName: "Hanako", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, 10, result[0].DMMID)
	assert.Equal(t, "http://thumb.jpg", result[0].ThumbURL)
}

// --- Phase 2: scraped actress with JP name only, no NFO actress has that JP name ---

func TestMergeActressSlices_Phase2_JPNameNotInNFOIndices(t *testing.T) {
	// Scraped has JP name "X" that no NFO actress shares.
	// findNFOMatch: JP name key doesn't exist in nfoIndicesByJpName → range loop not entered.
	// But the function is called and returns -1 (already covered by existing tests).
	scraped := []models.Actress{
		{JapaneseName: "ユニーク", DMMID: 10},
	}
	nfo := []models.Actress{
		{JapaneseName: "別人", FirstName: "Other"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2)
}

// --- Mixed matching: some match by JP name, some by romanized, some unmatched ---

func TestMergeActressSlices_MixedMatchTypes(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "田中", DMMID: 1, ThumbURL: "sc1.jpg"},
		{FirstName: "Yui", LastName: "Hatano", DMMID: 2, ThumbURL: "sc2.jpg"},
		{JapaneseName: "ユニーク", DMMID: 3, ThumbURL: "sc3.jpg"},
	}
	nfo := []models.Actress{
		{JapaneseName: "田中", ThumbURL: "nfo1.jpg"},
		{FirstName: "Yui", LastName: "Hatano", ThumbURL: "nfo2.jpg"},
		{JapaneseName: "別人", ThumbURL: "nfo3.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	// Actress 1: match by JP name → scraped thumb preserved
	// Actress 2: match by romanized name → scraped thumb preserved
	// Actress 3 (scraped): unmatched → appears
	// Actress 3 (NFO): unmatched → appears
	assert.Len(t, result, 4)

	for _, a := range result {
		if a.DMMID == 1 {
			assert.Equal(t, "sc1.jpg", a.ThumbURL, "scraped thumb should be preserved when non-empty")
		} else if a.DMMID == 2 {
			assert.Equal(t, "sc2.jpg", a.ThumbURL)
		}
	}
}

// --- Actress with empty strings for all name fields ---

func TestMergeActressSlices_EmptyNameFields_NoCrash(t *testing.T) {
	scraped := []models.Actress{
		{DMMID: 1, JapaneseName: "", FirstName: "", LastName: ""},
	}
	nfo := []models.Actress{
		{DMMID: 0, JapaneseName: "", FirstName: "", LastName: "", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2, "actresses with no identifiers should not match")
}

// --- preferNFO=false with scraped having empty name fields, NFO fills them ---

func TestMergeActressSlices_PreferNFOFalse_ScrapedEmptyNamesFilledFromNFO(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, FirstName: "", LastName: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "FilledFirst", LastName: "FilledLast"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, "FilledFirst", result[0].FirstName)
	assert.Equal(t, "FilledLast", result[0].LastName)
	assert.Equal(t, 10, result[0].DMMID)
}

// --- preferNFO=true with scraped having non-empty name fields, NFO overwrites ---

func TestMergeActressSlices_PreferNFOTrue_NFOOverwritesNonEmptyScraped(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, FirstName: "ScraperFirst", LastName: "ScraperLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "NFOFirst", LastName: "NFOLast"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	require.Len(t, result, 1)
	assert.Equal(t, "NFOFirst", result[0].FirstName)
	assert.Equal(t, "NFOLast", result[0].LastName)
	assert.Equal(t, 10, result[0].DMMID)
}

// --- preferNFO=true with NFO having empty FirstName, scraped has value ---

func TestMergeActressSlices_PreferNFOTrue_NFOEmptyNameNotOverwritten(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, FirstName: "ScraperFirst", LastName: "ScraperLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "", LastName: "NFOLast"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	require.Len(t, result, 1)
	// preferNFO=true: NFO's JapaneseName is empty → don't overwrite (already handled by if check)
	// NFO's FirstName is empty → don't overwrite
	// NFO's LastName is "NFOLast" → overwrite scraped's "ScraperLast"
	assert.Equal(t, "ScraperFirst", result[0].FirstName, "empty NFO FirstName should not overwrite scraped")
	assert.Equal(t, "NFOLast", result[0].LastName, "non-empty NFO LastName should overwrite scraped")
}

// --- preferNFO=false with scraped having JapaneseName, NFO also has JapaneseName ---

func TestMergeActressSlices_PreferNFOFalse_ScrapedJPNamePreserved(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, FirstName: "A"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "B"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	// preferNFO=false: scraped's JP name is non-empty, so NFO's JP name doesn't overwrite
	assert.Equal(t, "テスト", result[0].JapaneseName)
	assert.Equal(t, "A", result[0].FirstName, "scraped FirstName should be preserved when non-empty")
}

// --- Multiple scraped actresses matching different NFO actresses in Phase 2 range loop ---

func TestMergeActressSlices_Phase2_MultipleScrapedWithSameJP(t *testing.T) {
	// 4 scraped actresses with JP "X", 2 NFO actresses with JP "X".
	// Phase 1: NFO[0] → scraped[0], NFO[1] → scraped[1].
	// Phase 2: scraped[2] and scraped[3] try findNFOMatch with JP "X".
	//   nfoIndicesByJpName["jp:x"] has 2 entries, both matched → -1.
	scraped := []models.Actress{
		{JapaneseName: "共通", DMMID: 1},
		{JapaneseName: "共通", DMMID: 2},
		{JapaneseName: "共通", DMMID: 3},
		{JapaneseName: "共通", DMMID: 4},
	}
	nfo := []models.Actress{
		{JapaneseName: "共通", ThumbURL: "http://t1.jpg"},
		{JapaneseName: "共通", ThumbURL: "http://t2.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 4)

	// 2 actresses should have thumbs from NFO
	thumbCount := 0
	for _, a := range result {
		if a.ThumbURL != "" {
			thumbCount++
		}
	}
	assert.Equal(t, 2, thumbCount)
}

// --- Phase 2: findNFOMatch romanized with reversed key lookup ---

func TestMergeActressSlices_Phase2_RomanizedReversedKeyLookup(t *testing.T) {
	// Scraped has romanized "A B" (two actresses), NFO has romanized "B A" (reversed).
	// Phase 1: NFO looks up "name:b|a" → not found, then "name:a|b" → found in scraped.
	// Phase 2: second scraped looks up "name:a|b" in nfoIndicesByRomanizedName →
	//   NFO is indexed as "name:b|a", so "name:a|b" not found directly.
	//   Then checks reversed "name:b|a" → found but matched → -1.
	scraped := []models.Actress{
		{FirstName: "A", LastName: "B", DMMID: 1},
		{FirstName: "A", LastName: "B", DMMID: 2},
	}
	nfo := []models.Actress{
		{FirstName: "B", LastName: "A", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2)

	thumbCount := 0
	for _, a := range result {
		if a.ThumbURL == "http://thumb.jpg" {
			thumbCount++
		}
	}
	assert.Equal(t, 1, thumbCount)
}

// --- Large number of actresses stress test ---

func TestMergeActressSlices_ManyActresses(t *testing.T) {
	scraped := make([]models.Actress, 10)
	nfo := make([]models.Actress, 10)
	for i := 0; i < 10; i++ {
		scraped[i] = models.Actress{
			JapaneseName: "女優" + string(rune('A'+i)),
			DMMID:        i + 1,
			ThumbURL:     "http://sc.jpg",
		}
		nfo[i] = models.Actress{
			JapaneseName: "女優" + string(rune('A'+i)),
			FirstName:    "First" + string(rune('A'+i)),
		}
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 10)

	for _, a := range result {
		assert.NotEqual(t, 0, a.DMMID)
		assert.NotEmpty(t, a.FirstName, "FirstName should be filled from NFO")
	}
}

// --- Verify result ordering: scraped entries come first ---

func TestMergeActressSlices_ResultOrdering(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "A", DMMID: 1},
		{JapaneseName: "C", DMMID: 3},
	}
	nfo := []models.Actress{
		{JapaneseName: "B", DMMID: 0},
		{JapaneseName: "D", DMMID: 0},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 4)
	// All four should appear (no matches)
	// Scraped entries appear before NFO entries in the result
	assert.Equal(t, 1, result[0].DMMID)
	assert.Equal(t, 3, result[1].DMMID)
}
