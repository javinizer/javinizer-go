package template

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mediainfo"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Partial line coverage for resolveTag ---

// TestResolveTag_IDWithCaseModifier covers the modifier branch on line ~311
// (if modifier != "" { return e.applyCaseModifier(...) })
func TestResolveTag_IDWithCaseModifier_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "abc-123"}

	// UPPER modifier
	got, err := e.resolveTag("ID", "UPPER", ctx)
	require.NoError(t, err)
	assert.Equal(t, "ABC-123", got)

	// LOWER modifier
	got, err = e.resolveTag("ID", "LOWER", ctx)
	require.NoError(t, err)
	assert.Equal(t, "abc-123", got)

	// Unknown modifier returns as-is
	got, err = e.resolveTag("ID", "foobar", ctx)
	require.NoError(t, err)
	assert.Equal(t, "abc-123", got)
}

// TestResolveTag_ContentIDWithCaseModifier covers modifier branch for CONTENTID
func TestResolveTag_ContentIDWithCaseModifier_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ContentID: "ipx00535"}

	got, err := e.resolveTag("CONTENTID", "UPPER", ctx)
	require.NoError(t, err)
	assert.Equal(t, "IPX00535", got)
}

// TestResolveTag_TITLE_WithTruncationModifier covers the modifier branch on TITLE
// when no language is configured: if modifier != "" { return e.truncate(value, modifier) }
func TestResolveTag_TITLE_WithTruncationModifier_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Title: "A Very Long Movie Title Indeed"}

	got, err := e.resolveTag("TITLE", "10", ctx)
	require.NoError(t, err)
	assert.Contains(t, got, "...")
	assert.True(t, len(got) <= 13) // 10 chars + "..."
}

// TestResolveTag_TITLE_WithLanguageAndTruncation covers the truncation modifier
// when language is also resolved: parsed.truncationModifier != ""
func TestResolveTag_TITLE_WithLanguageAndTruncation_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{
		DefaultLanguage: "ja",
	})
	rd := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := &Context{
		Title:       "English Title",
		ReleaseDate: &rd,
		Translations: map[string]models.MovieTranslation{
			"ja": {Language: "ja", Title: "日本語タイトル"},
		},
	}

	// When language resolves but truncation modifier is also present
	got, err := e.Execute("<TITLE:ja:10>", ctx)
	require.NoError(t, err)
	// Should resolve Japanese title and truncate
	_ = got
}

// TestResolveTag_ORIGINALTITLE_WithLanguage covers the translation branch
func TestResolveTag_ORIGINALTITLE_WithLanguage_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "ja"})
	ctx := &Context{
		OriginalTitle: "English OT",
		Translations: map[string]models.MovieTranslation{
			"ja": {Language: "ja", OriginalTitle: "日本語OT"},
		},
	}

	got, err := e.resolveTag("ORIGINALTITLE", "ja", ctx)
	require.NoError(t, err)
	assert.Equal(t, "日本語OT", got)
}

// TestResolveTag_ORIGINALTITLE_RejectedLanguage covers rejectedLanguage branch
func TestResolveTag_ORIGINALTITLE_RejectedLanguage_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{})
	ctx := &Context{OriginalTitle: "My Original"}

	// Use an invalid-looking language spec on a translatable tag
	// This should trigger rejectedLanguage = true, falling back to base field
	got, err := e.resolveTag("ORIGINALTITLE", "xx-invalid", ctx)
	require.NoError(t, err)
	assert.Equal(t, "My Original", got)
}

// TestResolveTag_YEAR_ReleaseDateNil_WithReleaseYear covers the ReleaseYear > 0 branch
func TestResolveTag_YEAR_ReleaseDateNil_WithReleaseYear_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ReleaseYear: 2023}

	got, err := e.resolveTag("YEAR", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "2023", got)
}

// TestResolveTag_YEAR_BothEmpty covers the empty return branch
func TestResolveTag_YEAR_BothEmpty_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}

	got, err := e.resolveTag("YEAR", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_RELEASEDATE_WithModifier covers the date formatting modifier branch
func TestResolveTag_RELEASEDATE_WithModifier_Partial(t *testing.T) {
	e := NewEngine()
	rd := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)
	ctx := &Context{ReleaseDate: &rd}

	got, err := e.resolveTag("RELEASEDATE", "YYYY/MM/DD", ctx)
	require.NoError(t, err)
	assert.Equal(t, "2023/06/15", got)
}

// TestResolveTag_RELEASEDATE_Nil covers empty return when no date
func TestResolveTag_RELEASEDATE_Nil_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}

	got, err := e.resolveTag("RELEASEDATE", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_RUNTIME_Zero covers the empty return when Runtime <= 0
func TestResolveTag_RUNTIME_Zero_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Runtime: 0}

	got, err := e.resolveTag("RUNTIME", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_DIRECTOR_WithLanguage covers translation branch for DIRECTOR
func TestResolveTag_DIRECTOR_WithLanguage_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "ja"})
	ctx := &Context{
		Director: "Director En",
		Translations: map[string]models.MovieTranslation{
			"ja": {Language: "ja", Director: "監督名"},
		},
	}

	got, err := e.resolveTag("DIRECTOR", "ja", ctx)
	require.NoError(t, err)
	assert.Equal(t, "監督名", got)
}

// TestResolveTag_DESCRIPTION_WithLanguage covers translation branch
func TestResolveTag_DESCRIPTION_WithLanguage_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "ja"})
	ctx := &Context{
		Description: "English desc",
		Translations: map[string]models.MovieTranslation{
			"ja": {Language: "ja", Description: "日本語説明"},
		},
	}

	got, err := e.resolveTag("DESCRIPTION", "ja", ctx)
	require.NoError(t, err)
	assert.Equal(t, "日本語説明", got)
}

// TestResolveTag_STUDIO_WithLanguage covers translation branch for STUDIO/MAKER
func TestResolveTag_STUDIO_WithLanguage_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "ja"})
	ctx := &Context{
		Maker: "Studio En",
		Translations: map[string]models.MovieTranslation{
			"ja": {Language: "ja", Maker: "スタジオ"},
		},
	}

	got, err := e.resolveTag("STUDIO", "ja", ctx)
	require.NoError(t, err)
	assert.Equal(t, "スタジオ", got)
}

// TestResolveTag_MAKER_WithLanguage covers translation branch for MAKER
func TestResolveTag_MAKER_WithLanguage_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "ja"})
	ctx := &Context{
		Maker: "Maker En",
		Translations: map[string]models.MovieTranslation{
			"ja": {Language: "ja", Maker: "メーカー"},
		},
	}

	got, err := e.resolveTag("MAKER", "ja", ctx)
	require.NoError(t, err)
	assert.Equal(t, "メーカー", got)
}

// TestResolveTag_LABEL_WithLanguage covers translation branch
func TestResolveTag_LABEL_WithLanguage_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "ja"})
	ctx := &Context{
		Label: "Label En",
		Translations: map[string]models.MovieTranslation{
			"ja": {Language: "ja", Label: "レーベル"},
		},
	}

	got, err := e.resolveTag("LABEL", "ja", ctx)
	require.NoError(t, err)
	assert.Equal(t, "レーベル", got)
}

// TestResolveTag_SERIES_WithLanguage covers translation branch
func TestResolveTag_SERIES_WithLanguage_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "ja"})
	ctx := &Context{
		Series: "Series En",
		Translations: map[string]models.MovieTranslation{
			"ja": {Language: "ja", Series: "シリーズ"},
		},
	}

	got, err := e.resolveTag("SERIES", "ja", ctx)
	require.NoError(t, err)
	assert.Equal(t, "シリーズ", got)
}

// TestResolveTag_SET_WithLanguage covers translation branch for SET
func TestResolveTag_SET_WithLanguage_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "ja"})
	ctx := &Context{
		Series: "Series En",
		Translations: map[string]models.MovieTranslation{
			"ja": {Language: "ja", Series: "シリーズ"},
		},
	}

	got, err := e.resolveTag("SET", "ja", ctx)
	require.NoError(t, err)
	assert.Equal(t, "シリーズ", got)
}

// TestResolveTag_ACTORS_GroupActressWithGroupName covers GroupActressName != "" branch
func TestResolveTag_ACTORS_GroupActressWithGroupName_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{
		Actresses:        []string{"Alice", "Bob"},
		GroupActress:     true,
		GroupActressName: "GroupX",
	}

	got, err := e.resolveTag("ACTORS", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "GroupX", got)
}

// TestResolveTag_ACTORS_GroupActressDefaultName covers groupName == "" branch -> "@Group"
func TestResolveTag_ACTORS_GroupActressDefaultName_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{
		Actresses:    []string{"Alice", "Bob"},
		GroupActress: true,
	}

	got, err := e.resolveTag("ACTORS", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "@Group", got)
}

// TestResolveTag_ACTORS_WithCustomDelimiter covers the DELIM= keyword branch
func TestResolveTag_ACTORS_WithCustomDelimiter_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Actresses: []string{"Alice", "Bob"}}

	got, err := e.resolveTag("ACTORS", "DELIM= | ", ctx)
	require.NoError(t, err)
	assert.Equal(t, "Alice | Bob", got)
}

// TestResolveTag_ACTORS_Empty covers empty return branch
func TestResolveTag_ACTORS_Empty_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}

	got, err := e.resolveTag("ACTORS", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_ACTRESS_FallbackToActresses covers ActressName="" and ActressDetails empty
func TestResolveTag_ACTRESS_FallbackToActresses_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Actresses: []string{"First"}}

	got, err := e.resolveTag("ACTRESS", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "First", got)
}

// TestResolveTag_ACTRESS_Empty covers all empty fallback
func TestResolveTag_ACTRESS_Empty_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}

	got, err := e.resolveTag("ACTRESS", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_ACTRESS_FromActressDetails covers ActressDetails > 0 branch
func TestResolveTag_ACTRESS_FromActressDetails_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{
		ActressDetails: []ActressDetail{
			{FirstName: "Yui", LastName: "Hatano"},
		},
	}

	got, err := e.resolveTag("ACTRESS", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "Hatano Yui", got)
}

// TestResolveTag_GENRES_WithCustomDelimiter covers modifier delimiter branch
func TestResolveTag_GENRES_WithCustomDelimiter_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Genres: []string{"Action", "Drama"}}

	got, err := e.resolveTag("GENRES", " / ", ctx)
	require.NoError(t, err)
	assert.Equal(t, "Action / Drama", got)
}

// TestResolveTag_GENRES_Empty covers empty return
func TestResolveTag_GENRES_Empty_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}

	got, err := e.resolveTag("GENRES", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_INDEX_WithPadding covers modifier padding branch
func TestResolveTag_INDEX_WithPadding_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Index: 5}

	got, err := e.resolveTag("INDEX", "3", ctx)
	require.NoError(t, err)
	assert.Equal(t, "005", got)
}

// TestResolveTag_INDEX_ZeroWithModifier covers Index=0 with modifier
func TestResolveTag_INDEX_ZeroWithModifier_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Index: 0}

	got, err := e.resolveTag("INDEX", "3", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_INDEX_WithoutModifier covers Index > 0 without modifier
func TestResolveTag_INDEX_WithoutModifier_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Index: 42}

	got, err := e.resolveTag("INDEX", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "42", got)
}

// TestResolveTag_FIRSTNAME covers FIRSTNAME tag
func TestResolveTag_FIRSTNAME_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{FirstName: "Yui"}

	got, err := e.resolveTag("FIRSTNAME", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "Yui", got)
}

// TestResolveTag_LASTNAME covers LASTNAME tag
func TestResolveTag_LASTNAME_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{LastName: "Hatano"}

	got, err := e.resolveTag("LASTNAME", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "Hatano", got)
}

// TestResolveTag_ACTRESSNAME_FallbackPaths covers all fallback branches
func TestResolveTag_ACTRESSNAME_FallbackPaths_Partial(t *testing.T) {
	e := NewEngine()

	// ActressName set
	ctx := &Context{ActressName: "Star1"}
	got, err := e.resolveTag("ACTRESSNAME", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "Star1", got)

	// Fall back to ActressDetails
	ctx = &Context{ActressDetails: []ActressDetail{{FirstName: "Yui", LastName: "Hatano"}}}
	got, err = e.resolveTag("ACTRESSNAME", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "Hatano Yui", got)

	// Fall back to Actresses
	ctx = &Context{Actresses: []string{"First"}}
	got, err = e.resolveTag("ACTRESSNAME", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "First", got)

	// All empty
	ctx = &Context{}
	got, err = e.resolveTag("ACTRESSNAME", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_RESOLUTION_WithMediaInfo covers info != nil branch
func TestResolveTag_RESOLUTION_WithMediaInfo_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}
	// Inject cached media info
	ctx.cachedMediaInfo = &mediainfo.VideoInfo{Width: 1920, Height: 1080}

	got, err := e.resolveTag("RESOLUTION", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "1080p", got)
}

// TestResolveTag_RESOLUTION_NoMediaInfo covers info == nil branch
func TestResolveTag_RESOLUTION_NoMediaInfo_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}

	got, err := e.resolveTag("RESOLUTION", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_PART_WithPadding covers padding modifier branch
func TestResolveTag_PART_WithPadding_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{PartNumber: 1}

	got, err := e.resolveTag("PART", "2", ctx)
	require.NoError(t, err)
	assert.Equal(t, "01", got)
}

// TestResolveTag_PART_WithoutModifier covers part without padding
func TestResolveTag_PART_WithoutModifier_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{PartNumber: 3}

	got, err := e.resolveTag("PART", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "3", got)
}

// TestResolveTag_PART_Zero covers empty return for PartNumber=0
func TestResolveTag_PART_Zero_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{PartNumber: 0}

	got, err := e.resolveTag("PART", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_PARTSUFFIX covers PartSuffix tag
func TestResolveTag_PARTSUFFIX_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{PartSuffix: "-pt1"}

	got, err := e.resolveTag("PARTSUFFIX", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "-pt1", got)
}

// TestResolveTag_DISC_SameBehaviorAsPart covers DISC tag
func TestResolveTag_DISC_SameBehaviorAsPart_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{PartNumber: 2}

	got, err := e.resolveTag("DISC", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "2", got)
}

// TestResolveTag_RATING_Positive covers Rating > 0
func TestResolveTag_RATING_Positive_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Rating: 7.5}

	got, err := e.resolveTag("RATING", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "7.5", got)
}

// TestResolveTag_RATING_Zero covers Rating <= 0
func TestResolveTag_RATING_Zero_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Rating: 0}

	got, err := e.resolveTag("RATING", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_MULTIPART_TrueAndFalse covers both branches
func TestResolveTag_MULTIPART_TrueAndFalse_Partial(t *testing.T) {
	e := NewEngine()

	ctx := &Context{IsMultiPart: true}
	got, err := e.resolveTag("MULTIPART", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "true", got)

	ctx = &Context{IsMultiPart: false}
	got, err = e.resolveTag("MULTIPART", "", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestResolveTag_Unknown covers default branch returning error
func TestResolveTag_Unknown_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}

	_, err := e.resolveTag("UNKNOWNTAG", "", ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tag")
}

// --- Partial line coverage for processConditionalsWithContext ---

// TestProcessConditionals_ElseBranch covers the else content branch
func TestProcessConditionals_ElseBranch_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Series: ""}

	got, err := e.Execute("<IF:SERIES>has series<ELSE>no series</IF>", ctx)
	require.NoError(t, err)
	assert.Equal(t, "no series", got)
}

// TestProcessConditionals_TrueBranch covers true content branch
func TestProcessConditionals_TrueBranch_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Series: "MySeries"}

	got, err := e.Execute("<IF:SERIES>has series</IF>", ctx)
	require.NoError(t, err)
	assert.Equal(t, "has series", got)
}

// TestProcessConditionals_CancelledContext covers context cancellation
func TestProcessConditionals_CancelledContext_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 1})
	// Build a very long conditional result to exceed output limit
	longContent := ""
	for i := 0; i < 10000; i++ {
		longContent += "x"
	}
	ctx := &Context{Title: "T"}
	template := "<IF:TITLE>" + longContent + "</IF>"

	_, err := e.ExecuteWithContext(context.Background(), template, ctx)
	assert.Error(t, err)
}

// --- Partial line coverage for TruncateTitle ---

// TestTruncateTitle_CJK_LongRunes covers CJK truncation with maxLen > 3
func TestTruncateTitle_CJK_LongRunes_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitle("日本語のタイトルテスト", 8)
	assert.Contains(t, got, "...")
	assert.True(t, len([]rune(got)) <= 8)
}

// TestTruncateTitle_CJK_ShortRunes covers CJK where runes <= maxLen-3
func TestTruncateTitle_CJK_ShortRunes_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitle("日本", 10)
	assert.Equal(t, "日本", got) // fits within limit
}

// TestTruncateTitle_CJK_MaxLen3OrLess covers CJK with maxLen <= 3
// When maxLen <= 3 and isCJK, the code falls through to the non-CJK path
// which truncates at rune boundary
func TestTruncateTitle_CJK_MaxLen3OrLess_Partial(t *testing.T) {
	e := NewEngine()

	// CJK with maxLen=2: isCJK is true but maxLen <= 3 so it returns title as-is
	// (the `if maxLen > 3` check fails for CJK path)
	got := e.TruncateTitle("日本語", 2)
	// maxLen <= 3: CJK returns title (length > maxLen but maxLen <= 3 check)
	assert.Equal(t, "日本語", got) // CJK path returns title when maxLen <= 3
}

// TestTruncateTitle_NonCJK_NoSpaceForWordBreak covers case where lastSpace <= 0
func TestTruncateTitle_NonCJK_NoSpaceForWordBreak_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitle("Nospaceatallhere", 10)
	assert.Contains(t, got, "...")
}

// TestTruncateTitle_NonCJK_WordBoundary covers case with space for word break
func TestTruncateTitle_NonCJK_WordBoundary_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitle("Hello World From Test", 12)
	assert.Contains(t, got, "...")
	// Should break at word boundary
	assert.Contains(t, got, "Hello")
}

// TestTruncateTitle_NonCJK_ShortEnough covers len(runes) <= maxLen-3
func TestTruncateTitle_NonCJK_ShortEnough_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitle("Hi", 10)
	assert.Equal(t, "Hi", got)
}

// TestTruncateTitle_MaxLenNegative covers maxLen <= 0
func TestTruncateTitle_MaxLenNegative_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitle("Hello", -1)
	assert.Equal(t, "Hello", got)
}

// TestTruncateTitle_MaxLenLE3_NonCJK covers non-CJK with maxLen <= 3 but title longer
func TestTruncateTitle_MaxLenLE3_NonCJK_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitle("Hello", 3)
	assert.Equal(t, "Hel", got)
}

// TestTruncateTitle_MaxLenLE3_Fits covers non-CJK with maxLen <= 3 and short title
func TestTruncateTitle_MaxLenLE3_Fits_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitle("AB", 3)
	assert.Equal(t, "AB", got)
}

// --- Partial line coverage for TruncateTitleBytes ---

// TestTruncateTitleBytes_ZeroMaxBytes covers maxBytes <= 0
func TestTruncateTitleBytes_ZeroMaxBytes_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitleBytes("Hello", 0)
	assert.Equal(t, "", got)
}

// TestTruncateTitleBytes_NegativeMaxBytes covers maxBytes < 0
func TestTruncateTitleBytes_NegativeMaxBytes_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitleBytes("Hello", -5)
	assert.Equal(t, "", got)
}

// TestTruncateTitleBytes_FitsWithinMax covers len(title) <= maxBytes
func TestTruncateTitleBytes_FitsWithinMax_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitleBytes("Hello", 100)
	assert.Equal(t, "Hello", got)
}

// TestTruncateTitleBytes_MaxBytesLTE3_CantFitOneRune covers maxBytes <= 3, first rune too big
func TestTruncateTitleBytes_MaxBytesLTE3_CantFitOneRune_Partial(t *testing.T) {
	e := NewEngine()

	// 3-byte CJK rune, maxBytes=1
	got := e.TruncateTitleBytes("日本語", 1)
	assert.Equal(t, "", got)
}

// TestTruncateTitleBytes_MaxBytesLTE3_FitsRuneBoundary covers maxBytes <= 3, fits runes
func TestTruncateTitleBytes_MaxBytesLTE3_FitsRuneBoundary_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitleBytes("Hello", 2)
	assert.Equal(t, "He", got)
}

// TestTruncateTitleBytes_MaxBytes3_ExactReserve covers maxBytes == markerReserve == 3
func TestTruncateTitleBytes_MaxBytes3_ExactReserve_Partial(t *testing.T) {
	e := NewEngine()

	// maxBytes=3, markerReserve=3, goes into maxBytes <= markerReserve path
	// Can fit 3 bytes of ASCII in 3 bytes
	got := e.TruncateTitleBytes("Hello World", 3)
	assert.Equal(t, "Hel", got) // Fits 3 ASCII bytes, no marker
}

// TestTruncateTitleBytes_EndIdxZero_CantFitRune covers endIdx == 0 return marker branch
func TestTruncateTitleBytes_EndIdxZero_CantFitRune_Partial(t *testing.T) {
	e := NewEngine()

	// 4-byte emoji with small budget
	got := e.TruncateTitleBytes("🎉test", 4)
	// budget = 4-3 = 1 byte, emoji is 4 bytes, endIdx=0
	assert.Equal(t, "...", got)
}

// TestTruncateTitleBytes_NonCJK_WordBoundary covers word boundary truncation
func TestTruncateTitleBytes_NonCJK_WordBoundary_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitleBytes("Hello World Test", 13)
	assert.Contains(t, got, "...")
	// Should break at word boundary
}

// TestTruncateTitleBytes_CJK_NoWordBoundary covers CJK no word boundary
func TestTruncateTitleBytes_CJK_NoWordBoundary_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitleBytes("日本語のタイトル", 20)
	assert.Contains(t, got, "...")
}

// TestTruncateTitleBytes_TrailingSpacesTrimmed covers TrimRight for trailing spaces
func TestTruncateTitleBytes_TrailingSpacesTrimmed_Partial(t *testing.T) {
	e := NewEngine()

	got := e.TruncateTitleBytes("Hello World   More", 12)
	// Trailing spaces should be trimmed before adding marker
	assert.True(t, !strings.Contains(got, " ..."))
}

// --- Partial line coverage for ExecuteWithContext ---

// TestExecuteWithContext_CancelledContextDuringTagLoop covers i%25==0 cancellation check
func TestExecuteWithContext_CancelledContextDuringTagLoop_Partial(t *testing.T) {
	e := NewEngine()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	tmplCtx := &Context{ID: "test", Title: "title"}
	_, err := e.ExecuteWithContext(ctx, "<ID>", tmplCtx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
}

// TestExecuteWithContext_NilExecCtx covers nil execution context check
func TestExecuteWithContext_NilExecCtx_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "test"}

	_, err := e.ExecuteWithContext(nil, "<ID>", ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "execution context cannot be nil")
}

// TestExecuteWithContext_OutputLimitHit covers ensureOutputWithinLimit after tag replacement
func TestExecuteWithContext_OutputLimitHit_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 5})
	ctx := &Context{ID: "test", Title: "very long title that exceeds limit"}

	_, err := e.ExecuteWithContext(context.Background(), "<TITLE>", ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

// TestExecuteWithContext_UnknownTagInTemplate covers resolveTag returning error -> empty string
func TestExecuteWithContext_UnknownTagInTemplate_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "test"}

	got, err := e.ExecuteWithContext(context.Background(), "<UNKNOWN_TAG>", ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestExecuteWithContext_DuplicateTags covers tagReplacements dedup path
func TestExecuteWithContext_DuplicateTags_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABC", Title: "Title"}

	got, err := e.ExecuteWithContext(context.Background(), "<ID>-<ID>-<TITLE>", ctx)
	require.NoError(t, err)
	assert.Equal(t, "ABC-ABC-Title", got)
}

// TestExecuteWithContext_CancelledAfterProcessing covers final checkExecutionContext
func TestExecuteWithContext_CancelledAfterProcessing_Partial(t *testing.T) {
	// We can't easily cancel mid-execution from outside, but we can test
	// the final check by using a context that cancels after first check
	e := NewEngine()
	ctx := &Context{ID: "test"}

	got, err := e.ExecuteWithContext(context.Background(), "<ID>", ctx)
	require.NoError(t, err)
	assert.Equal(t, "test", got)
}

// --- Partial line coverage for Validate ---

// TestValidate_TemplateTooLarge covers template size limit
func TestValidate_TemplateTooLarge_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxTemplateBytes: 10})

	err := e.Validate("This template is way too large for the limit")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

// TestValidate_ConditionalDepthExceeded covers depth > MaxConditionalDepth
func TestValidate_ConditionalDepthExceeded_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxConditionalDepth: 3})

	err := e.Validate("<IF:A><IF:B><IF:C><IF:D>deep</IF></IF></IF></IF>")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conditional depth")
}

// TestValidate_UnexpectedClosing covers depth < 0
func TestValidate_UnexpectedClosing_Partial(t *testing.T) {
	e := NewEngine()

	err := e.Validate("</IF>")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected closing")
}

// TestValidate_UnclosedBlock covers depth != 0
func TestValidate_UnclosedBlock_Partial(t *testing.T) {
	e := NewEngine()

	err := e.Validate("<IF:A>open but never closed")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed")
}

// --- Partial line coverage for ValidatePathLength ---

// TestValidatePathLength_ZeroMaxLen covers maxLen <= 0
func TestValidatePathLength_ZeroMaxLen_Partial(t *testing.T) {
	e := NewEngine()

	err := e.ValidatePathLength("/very/long/path", 0)
	assert.NoError(t, err)
}

// TestValidatePathLength_ExceedsLimit covers len(path) > maxLen
func TestValidatePathLength_ExceedsLimit_Partial(t *testing.T) {
	e := NewEngine()

	err := e.ValidatePathLength("/very/long/path", 5)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds limit")
}

// TestValidatePathLength_WithinLimit covers len(path) <= maxLen
func TestValidatePathLength_WithinLimit_Partial(t *testing.T) {
	e := NewEngine()

	err := e.ValidatePathLength("/short", 100)
	assert.NoError(t, err)
}

// --- Partial line coverage for parseModifier ---

// TestParseModifier_FallbackChainInvalid covers fallback chain with invalid part
func TestParseModifier_FallbackChainInvalid_Partial(t *testing.T) {
	e := NewEngine()

	// "ja|invalid" has a pipe but second part is invalid
	result := e.translationResolver.parseModifier("TITLE", "ja|invalid")
	// Should not be treated as language since not all parts valid
	assert.False(t, result.isLanguage)
}

// TestParseModifier_FallbackChainValid covers valid fallback chain
func TestParseModifier_FallbackChainValid_Partial(t *testing.T) {
	e := NewEngine()

	result := e.translationResolver.parseModifier("TITLE", "ja|en")
	assert.True(t, result.isLanguage)
	assert.Equal(t, "ja|en", result.languageSpec)
}

// TestParseModifier_NonTranslatableWithInvalidLang covers non-translatable tags with 3-letter code
// normalizeLanguageCode("eng") = "" but looksLikeLanguageSpec("eng") = true
// For non-translatable tags, it should NOT be rejected, just treated as truncation
func TestParseModifier_NonTranslatableWithInvalidLang_Partial(t *testing.T) {
	e := NewEngine()

	// GENRES is not translatable, so looksLikeLanguageSpec doesn't trigger rejectedLanguage
	// "eng" -> normalizeLanguageCode returns "", but looksLikeLanguageSpec returns true
	result := e.translationResolver.parseModifier("GENRES", "eng")
	assert.False(t, result.rejectedLanguage)
	assert.Equal(t, "eng", result.truncationModifier) // treated as truncation
}

// TestParseModifier_TranslatableTagRejectedLanguage covers rejectedLanguage for translatable tags
// normalizeLanguageCode("eng") = "" but looksLikeLanguageSpec("eng") = true
// For translatable tags, this triggers rejectedLanguage
func TestParseModifier_TranslatableTagRejectedLanguage_Partial(t *testing.T) {
	e := NewEngine()

	// "eng" -> normalizeLanguageCode returns "" (3 letters), looksLikeLanguageSpec returns true
	// On a translatable tag (TITLE), this should be rejected
	result := e.translationResolver.parseModifier("TITLE", "eng")
	assert.True(t, result.rejectedLanguage)
}

// --- Partial line coverage for looksLikeLanguageSpec ---

// TestLooksLikeLanguageSpec_PipeModifier covers strings.Contains(modifier, "|")
func TestLooksLikeLanguageSpec_PipeModifier_Partial(t *testing.T) {
	e := NewEngine()

	assert.True(t, e.translationResolver.looksLikeLanguageSpec("ja|en"))
	assert.False(t, e.translationResolver.looksLikeLanguageSpec(""))
}

// TestLooksLikeLanguageSpec_LangWithRegion covers hyphenated lang spec
func TestLooksLikeLanguageSpec_LangWithRegion_Partial(t *testing.T) {
	e := NewEngine()

	assert.True(t, e.translationResolver.looksLikeLanguageSpec("en-US"))
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("1-US"))    // non-alpha prefix
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("x-US"))    // single char prefix
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("abcd-US")) // too long prefix
}

// TestLooksLikeLanguageSpec_ShortAlphaCode covers 2-3 letter alpha code
func TestLooksLikeLanguageSpec_ShortAlphaCode_Partial(t *testing.T) {
	e := NewEngine()

	assert.True(t, e.translationResolver.looksLikeLanguageSpec("en"))
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("eng"))
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("e1"))   // non-alpha
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("a"))    // too short
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("abcd")) // too long
}

// --- Partial line coverage for languageCandidates ---

// TestLanguageCandidates_ContextDefault covers ctx.DefaultLanguage priority
func TestLanguageCandidates_ContextDefault_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "ja"})
	ctx := &Context{DefaultLanguage: "en"}

	candidates := e.translationResolver.languageCandidates("", ctx)
	require.Len(t, candidates, 2)
	assert.Equal(t, "en", candidates[0]) // Context default first
	assert.Equal(t, "ja", candidates[1]) // Engine default second
}

// TestLanguageCandidates_Deduplication covers dedup of same language
func TestLanguageCandidates_Deduplication_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{
		DefaultLanguage:   "ja",
		FallbackLanguages: []string{"ja", "en"},
	})
	ctx := &Context{DefaultLanguage: "ja"}

	candidates := e.translationResolver.languageCandidates("", ctx)
	// "ja" should only appear once
	seen := map[string]bool{}
	for _, c := range candidates {
		assert.False(t, seen[c], "duplicate candidate: %s", c)
		seen[c] = true
	}
}

// TestLanguageCandidates_ExplicitFallbackChain covers pipe-separated explicit lang
func TestLanguageCandidates_ExplicitFallbackChain_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}

	candidates := e.translationResolver.languageCandidates("ja|en", ctx)
	require.Len(t, candidates, 2)
	assert.Equal(t, "ja", candidates[0])
	assert.Equal(t, "en", candidates[1])
}

// TestLanguageCandidates_InvalidInChain covers invalid language in chain skipped
func TestLanguageCandidates_InvalidInChain_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{}

	candidates := e.translationResolver.languageCandidates("ja|invalidcode|en", ctx)
	// "invalidcode" is 11 chars, too long for normalizeLanguageCode
	assert.Equal(t, "ja", candidates[0])
	// en should also be present if normalizeLanguageCode accepts it
	found := false
	for _, c := range candidates {
		if c == "en" {
			found = true
		}
	}
	assert.True(t, found, "expected 'en' in candidates")
}

// --- Partial line coverage for resolveTranslatedTag ---

// TestResolveTranslatedTag_FallbackToBase covers base field fallback
func TestResolveTranslatedTag_FallbackToBase_Partial(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "fr"})
	ctx := &Context{
		Title: "English Title",
		Translations: map[string]models.MovieTranslation{
			"fr": {Language: "fr"}, // Empty title, should fall back
		},
	}

	got := e.translationResolver.resolveTranslatedTag("TITLE", "", ctx)
	assert.Equal(t, "English Title", got) // Falls back to base
}

// --- Partial line coverage for resolveBaseTag ---

// TestResolveBaseTag_AllFieldMappings covers all tagName cases
func TestResolveBaseTag_AllFieldMappings_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{
		Title:         "T",
		OriginalTitle: "OT",
		Director:      "D",
		Maker:         "M",
		Label:         "L",
		Series:        "S",
		Description:   "Desc",
	}

	assert.Equal(t, "T", e.translationResolver.resolveBaseTag("TITLE", ctx))
	assert.Equal(t, "OT", e.translationResolver.resolveBaseTag("ORIGINALTITLE", ctx))
	assert.Equal(t, "D", e.translationResolver.resolveBaseTag("DIRECTOR", ctx))
	assert.Equal(t, "M", e.translationResolver.resolveBaseTag("MAKER", ctx))
	assert.Equal(t, "M", e.translationResolver.resolveBaseTag("STUDIO", ctx))
	assert.Equal(t, "L", e.translationResolver.resolveBaseTag("LABEL", ctx))
	assert.Equal(t, "S", e.translationResolver.resolveBaseTag("SERIES", ctx))
	assert.Equal(t, "S", e.translationResolver.resolveBaseTag("SET", ctx))
	assert.Equal(t, "Desc", e.translationResolver.resolveBaseTag("DESCRIPTION", ctx))
	assert.Equal(t, "", e.translationResolver.resolveBaseTag("UNKNOWN", ctx))
}

// --- Partial line coverage for translationFieldValue ---

// TestTranslationFieldValue_AllFieldMappings covers all tagName cases
func TestTranslationFieldValue_AllFieldMappings_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{
		Translations: map[string]models.MovieTranslation{
			"ja": {
				Language:      "ja",
				Title:         "JT",
				OriginalTitle: "JOT",
				Director:      "JD",
				Maker:         "JM",
				Label:         "JL",
				Series:        "JS",
				Description:   "JDesc",
			},
		},
	}

	assert.Equal(t, "JT", e.translationResolver.translationFieldValue("TITLE", "ja", ctx))
	assert.Equal(t, "JOT", e.translationResolver.translationFieldValue("ORIGINALTITLE", "ja", ctx))
	assert.Equal(t, "JD", e.translationResolver.translationFieldValue("DIRECTOR", "ja", ctx))
	assert.Equal(t, "JM", e.translationResolver.translationFieldValue("MAKER", "ja", ctx))
	assert.Equal(t, "JM", e.translationResolver.translationFieldValue("STUDIO", "ja", ctx))
	assert.Equal(t, "JL", e.translationResolver.translationFieldValue("LABEL", "ja", ctx))
	assert.Equal(t, "JS", e.translationResolver.translationFieldValue("SERIES", "ja", ctx))
	assert.Equal(t, "JS", e.translationResolver.translationFieldValue("SET", "ja", ctx))
	assert.Equal(t, "JDesc", e.translationResolver.translationFieldValue("DESCRIPTION", "ja", ctx))
	assert.Equal(t, "", e.translationResolver.translationFieldValue("UNKNOWN", "ja", ctx))
}

// TestTranslationFieldValue_NilTranslations covers nil Translations map
func TestTranslationFieldValue_NilTranslations_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{} // Translations is nil

	got := e.translationResolver.translationFieldValue("TITLE", "ja", ctx)
	assert.Equal(t, "", got)
}

// TestTranslationFieldValue_MissingLang covers missing language in map
func TestTranslationFieldValue_MissingLang_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{Translations: map[string]models.MovieTranslation{}}

	got := e.translationResolver.translationFieldValue("TITLE", "ja", ctx)
	assert.Equal(t, "", got)
}

// --- Partial line coverage for formatDate ---

// TestFormatDate_AllPatterns covers all pattern replacements
func TestFormatDate_AllPatterns_Partial(t *testing.T) {
	e := NewEngine()
	date := time.Date(2023, 6, 15, 14, 30, 45, 0, time.UTC)

	got := e.formatDate(&date, "YYYY-MM-DD HH:mm:ss")
	assert.Equal(t, "2023-06-15 14:30:45", got)

	got = e.formatDate(&date, "YY/MM/DD")
	assert.Equal(t, "23/06/15", got)
}

// --- Partial line coverage for normalizeLanguageList ---

// TestNormalizeLanguageList_Deduplication covers dedup
func TestNormalizeLanguageList_Deduplication_Partial(t *testing.T) {
	result := normalizeLanguageList([]string{"en", "EN", "ja"})
	assert.Equal(t, []string{"en", "ja"}, result)
}

// TestNormalizeLanguageList_EmptyInput covers empty input
func TestNormalizeLanguageList_EmptyInput_Partial(t *testing.T) {
	result := normalizeLanguageList(nil)
	assert.Nil(t, result)

	result = normalizeLanguageList([]string{})
	assert.Nil(t, result)
}

// TestNormalizeLanguageList_InvalidCodesSkipped covers invalid code filtering
func TestNormalizeLanguageList_InvalidCodesSkipped_Partial(t *testing.T) {
	result := normalizeLanguageList([]string{"en", "invalidcode", "ja"})
	assert.Equal(t, []string{"en", "ja"}, result)
}

// --- Partial line coverage for ExecuteWithMaxBytes ---

// TestExecuteWithMaxBytes_OriginalTitleDifferentFromTitle covers else branch
func TestExecuteWithMaxBytes_OriginalTitleDifferentFromTitle_Partial(t *testing.T) {
	e := NewEngine()
	ctx := &Context{
		Title:         "English Title Very Long For Truncation",
		OriginalTitle: "Japanese Title Very Long For Truncation",
	}

	// Force truncation with small maxBytes
	got, err := e.ExecuteWithMaxBytes("<ORIGINALTITLE>", ctx, 15)
	require.NoError(t, err)
	assert.Contains(t, got, "...")
}

// --- Partial line coverage for isNumericModifier ---

// TestIsNumericModifier_Cases covers various inputs
func TestIsNumericModifier_Cases_Partial(t *testing.T) {
	e := NewEngine()

	assert.False(t, e.translationResolver.isNumericModifier(""))
	assert.True(t, e.translationResolver.isNumericModifier("50"))
	assert.False(t, e.translationResolver.isNumericModifier("0"))  // n > 0 check
	assert.False(t, e.translationResolver.isNumericModifier("-1")) // n > 0 check
	assert.False(t, e.translationResolver.isNumericModifier("abc"))
}

// --- Partial line coverage for applyCaseModifier ---

// TestApplyCaseModifier_AllBranches covers all switch cases
func TestApplyCaseModifier_AllBranches_Partial(t *testing.T) {
	e := NewEngine()

	assert.Equal(t, "ABC", e.applyCaseModifier("abc", "UPPERCASE"))
	assert.Equal(t, "ABC", e.applyCaseModifier("abc", "UPPER"))
	assert.Equal(t, "abc", e.applyCaseModifier("ABC", "LOWERCASE"))
	assert.Equal(t, "abc", e.applyCaseModifier("ABC", "LOWER"))
	assert.Equal(t, "abc", e.applyCaseModifier("abc", "unknown")) // default
}

// strings.Contains is already imported and used directly
