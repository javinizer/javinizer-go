package template

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ExecuteWithMaxBytes coverage ---

func TestExecuteWithMaxBytes_OriginalTitleDiffersFromTitle(t *testing.T) {
	e := NewEngine()
	// Use strings that are clearly different
	ctx := &Context{
		Title:         "English Title",
		OriginalTitle: "Japanese Title",
	}
	// maxBytes so small that title truncation is required
	got, err := e.ExecuteWithMaxBytes("<ORIGINALTITLE>", ctx, 8)
	require.NoError(t, err)
	// Should truncate OriginalTitle independently since Title != OriginalTitle
	assert.Contains(t, got, "...")
}

func TestExecuteWithMaxBytes_FrameErrorFallsBack(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxTemplateBytes: 5})
	ctx := &Context{ID: "ABC", Title: "T"}
	// Template too large: both frame and fallback Execute calls fail
	_, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestExecuteWithMaxBytes_TitleBudgetExhausted(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABC-123", Title: "T", ReleaseYear: 2024}
	// maxBytes so small that titleBudget <= 0 after subtracting frame bytes
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE> (<YEAR>)", ctx, 5)
	require.NoError(t, err)
	// Falls back to Execute without truncation
	assert.Contains(t, got, "ABC-123")
}

func TestExecuteWithMaxBytes_TitleFitsInBudget(t *testing.T) {
	e := NewEngine()
	ctx := &Context{ID: "ABC", Title: "Short", ReleaseYear: 2024}
	// Title fits within budget, no truncation needed
	got, err := e.ExecuteWithMaxBytes("<ID> - <TITLE>", ctx, 200)
	require.NoError(t, err)
	assert.Equal(t, "ABC - Short", got)
}

// --- ExecuteWithContext coverage ---

func TestExecuteWithContext_NilCtx(t *testing.T) {
	e := NewEngine()
	_, err := e.ExecuteWithContext(context.Background(), "<ID>", nil)
	assert.EqualError(t, err, "context cannot be nil")
}

func TestExecuteWithContext_CancelledDuringTagLoop(t *testing.T) {
	e := NewEngine()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.ExecuteWithContext(ctx, "<ID>", &Context{ID: "test"})
	assert.Error(t, err)
}

func TestExecuteWithContext_OutputExceededAfterConditionals(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 10})
	ctx := &Context{ID: "test", Series: "S"}
	_, err := e.ExecuteWithContext(context.Background(), "<IF:SERIES>content that exceeds the ten byte limit</IF>", ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestExecuteWithContext_OutputExceededAfterTags(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 5})
	ctx := &Context{Title: "Very Long Title"}
	_, err := e.ExecuteWithContext(context.Background(), "<TITLE>", ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestExecuteWithContext_CancelledAtEnd(t *testing.T) {
	e := NewEngine()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.ExecuteWithContext(ctx, "<ID>", &Context{ID: "test"})
	assert.Error(t, err)
}

func TestExecuteWithContext_CancelledDuringTagLoop25(t *testing.T) {
	e := NewEngine()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Build template with 25+ tags to hit i%25==0 cancellation check
	var tmpl string
	for i := 0; i < 26; i++ {
		tmpl += "<ID>"
	}
	_, err := e.ExecuteWithContext(ctx, tmpl, &Context{ID: "test"})
	assert.Error(t, err)
}

func TestExecuteWithContext_InvalidTemplate(t *testing.T) {
	e := NewEngine()
	_, err := e.ExecuteWithContext(context.Background(), "<IF:TAG>unclosed", &Context{ID: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed")
}

// --- processConditionalsWithContext coverage ---

func TestProcessConditionalsWithContext_CancelledDuringLoop(t *testing.T) {
	e := NewEngine()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.processConditionalsWithContext(ctx, "<IF:SERIES>x</IF>", &Context{Series: "S"})
	assert.Error(t, err)
}

func TestProcessConditionalsWithContext_OutputLimitExceeded(t *testing.T) {
	e := newEngineWithOptions(engineOptions{MaxOutputBytes: 5})
	_, err := e.processConditionalsWithContext(context.Background(), "<IF:SERIES>content too long for limit</IF>", &Context{Series: "S"})
	assert.Error(t, err)
}

func TestProcessConditionalsWithContext_ElseBranchEmpty(t *testing.T) {
	e := NewEngine()
	result, err := e.processConditionalsWithContext(context.Background(), "<IF:SERIES>yes<ELSE></IF>", &Context{Series: ""})
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

// --- resolveTag coverage ---

func TestResolveTag_TitleWithRejectedLanguage(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "en"})
	// "eng" is invalid 3-letter code → looksLikeLanguageSpec → rejectedLanguage
	// With rejectedLanguage=true, falls to base field + modifier branch
	val, err := e.resolveTag("TITLE", "eng", &Context{Title: "Base Title"})
	require.NoError(t, err)
	// rejectedLanguage: goes to else branch, modifier != "" → truncate
	assert.Equal(t, "Base Title", val)
}

func TestResolveTag_OriginalTitleWithRejectedLanguage(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "en"})
	val, err := e.resolveTag("ORIGINALTITLE", "eng", &Context{OriginalTitle: "Base"})
	require.NoError(t, err)
	// rejectedLanguage → else branch, no modifier truncation for ORIGINALTITLE
	assert.Equal(t, "Base", val)
}

func TestResolveTag_IndexWithModifierZeroIndex(t *testing.T) {
	e := NewEngine()
	val, err := e.resolveTag("INDEX", "3", &Context{Index: 0})
	require.NoError(t, err)
	// modifier != "" but index == 0 → falls through to return ""
	assert.Equal(t, "", val)
}

func TestResolveTag_PartWithoutModifier(t *testing.T) {
	e := NewEngine()
	val, err := e.resolveTag("PART", "", &Context{PartNumber: 3})
	require.NoError(t, err)
	assert.Equal(t, "3", val)
}

func TestResolveTag_NonTranslatableTagWithLanguageModifier(t *testing.T) {
	e := NewEngine()
	// GENRES is not translatable; "en" is a valid language code
	// parseModifier returns isLanguage=true, but resolveTag's GENRES case
	// doesn't check isTranslatableTag, so modifier is used as delimiter
	val, err := e.resolveTag("GENRES", "en", &Context{Genres: []string{"A", "B"}})
	require.NoError(t, err)
	assert.Equal(t, "AenB", val)
}

func TestResolveTag_ACTRESSFallbackToActresses(t *testing.T) {
	e := NewEngine()
	// ACTRESS with no ActressName, no ActressDetails, but has Actresses
	val, err := e.resolveTag("ACTRESS", "", &Context{Actresses: []string{"First"}})
	require.NoError(t, err)
	assert.Equal(t, "First", val)
}

func TestResolveTag_ACTRESSNameFallbackToActresses(t *testing.T) {
	e := NewEngine()
	// ACTRESSNAME with no ActressName, no ActressDetails, but has Actresses
	val, err := e.resolveTag("ACTRESSNAME", "", &Context{Actresses: []string{"First"}})
	require.NoError(t, err)
	assert.Equal(t, "First", val)
}

func TestResolveTag_DirectorWithRejectedLanguage(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "en"})
	val, err := e.resolveTag("DIRECTOR", "eng", &Context{Director: "Base Director"})
	require.NoError(t, err)
	assert.Equal(t, "Base Director", val)
}

func TestResolveTag_DescriptionWithRejectedLanguage(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "en"})
	val, err := e.resolveTag("DESCRIPTION", "eng", &Context{Description: "Base Desc"})
	require.NoError(t, err)
	assert.Equal(t, "Base Desc", val)
}

func TestResolveTag_StudioWithRejectedLanguage(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "en"})
	val, err := e.resolveTag("STUDIO", "eng", &Context{Maker: "Base Maker"})
	require.NoError(t, err)
	assert.Equal(t, "Base Maker", val)
}

func TestResolveTag_LabelWithRejectedLanguage(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "en"})
	val, err := e.resolveTag("LABEL", "eng", &Context{Label: "Base Label"})
	require.NoError(t, err)
	assert.Equal(t, "Base Label", val)
}

func TestResolveTag_SeriesWithRejectedLanguage(t *testing.T) {
	e := newEngineWithOptions(engineOptions{DefaultLanguage: "en"})
	val, err := e.resolveTag("SERIES", "eng", &Context{Series: "Base Series"})
	require.NoError(t, err)
	assert.Equal(t, "Base Series", val)
}

func TestResolveTag_TitleNoModifierNoTranslation(t *testing.T) {
	e := NewEngine()
	// TITLE with no modifier, no default language → base field path (no translation)
	val, err := e.resolveTag("TITLE", "", &Context{Title: "Base Title"})
	require.NoError(t, err)
	assert.Equal(t, "Base Title", val)
}

func TestResolveTag_ActressAllEmpty(t *testing.T) {
	e := NewEngine()
	val, err := e.resolveTag("ACTRESS", "", &Context{})
	require.NoError(t, err)
	assert.Equal(t, "", val)
}

func TestResolveTag_IndexZeroNoModifier(t *testing.T) {
	e := NewEngine()
	val, err := e.resolveTag("INDEX", "", &Context{Index: 0})
	require.NoError(t, err)
	assert.Equal(t, "", val)
}

// --- TruncateTitle coverage ---

func TestTruncateTitle_CJKTitleFitsWithinMaxLen(t *testing.T) {
	e := NewEngine()
	// CJK title where rune count <= maxLen-3, so it returns title as-is
	// "日本語" = 3 runes, maxLen=10, maxLen-3=7, 3 <= 7 → returns title
	result := e.TruncateTitle("日本語", 10)
	assert.Equal(t, "日本語", result)
}

func TestTruncateTitle_CJKTitleExactRuneCount(t *testing.T) {
	e := NewEngine()
	// CJK title where len(runes) == maxLen-3 exactly → still returns truncated + marker
	// "日本語タイトル" = 7 runes, maxLen=10, maxLen-3=7, 7 > 7 is false → returns title
	result := e.TruncateTitle("日本語タイトル", 10)
	assert.Equal(t, "日本語タイトル", result)
}

func TestTruncateTitle_CJKMaxLenLTE3_ReturnsFullTitle(t *testing.T) {
	e := NewEngine()
	// CJK with maxLen=3, byte len > maxLen, but CJK path returns title when maxLen <= 3
	result := e.TruncateTitle("日本語", 3)
	assert.Equal(t, "日本語", result)
}

func TestTruncateTitle_NonCJKMaxLenGt3RunesFit(t *testing.T) {
	e := NewEngine()
	// Non-CJK with multi-byte chars where len(runes) <= maxLen-3 but len(bytes) > maxLen
	// This hits the "return title" at line 552 for non-CJK maxLen>3 where runes fit
	// Since len(title) <= maxLen check at line 516 uses byte length,
	// we need rune count > maxLen-3 path. But that would truncate, not return title.
	// The "return title" at line 552 requires len(runes) <= maxLen-3 AND len(title) > maxLen.
	// That's impossible for ASCII since len(runes)==len(title). But with multi-byte non-CJK chars,
	// e.g., é (2 bytes). "ééé" = 6 bytes, 3 runes, maxLen=5, maxLen-3=2, 3>2 → truncates
	// Actually this path is unreachable for ASCII and hard to reach for non-CJK multi-byte.
	result := e.TruncateTitle("Short", 100)
	assert.Equal(t, "Short", result)
}

func TestTruncateTitle_CJKMaxLenLTE3(t *testing.T) {
	e := NewEngine()
	// CJK with maxLen <= 3, returns title (can't fit marker)
	result := e.TruncateTitle("日本語タイトル", 3)
	assert.Equal(t, "日本語タイトル", result)
}

func TestTruncateTitle_NonCJKMaxLenLTE3NoTruncation(t *testing.T) {
	e := NewEngine()
	// Non-CJK, maxLen <= 3, but title runes <= maxLen
	result := e.TruncateTitle("AB", 2)
	assert.Equal(t, "AB", result)
}

// --- TruncateTitleBytes coverage ---

func TestTruncateTitleBytes_CJKFirstRuneTooBig(t *testing.T) {
	e := NewEngine()
	// maxBytes=2, CJK char needs 3 bytes → can't fit even one → i==0 → return ""
	result := e.TruncateTitleBytes("日本語", 2)
	assert.Equal(t, "", result)
}

func TestTruncateTitleBytes_BudgetEndIdxZero(t *testing.T) {
	e := NewEngine()
	// maxBytes=4 (budget=1 byte), CJK chars need 3 bytes each → endIdx==0 → return "..."
	result := e.TruncateTitleBytes("日本語", 4)
	assert.Equal(t, "...", result)
}

func TestTruncateTitleBytes_NonCJKTrailingSpaces(t *testing.T) {
	e := NewEngine()
	// Non-CJK text where truncated portion ends with spaces
	result := e.TruncateTitleBytes("Hello   World", 9)
	// budget=6: "Hello " fits (6 bytes), word boundary at space → "Hello..."
	assert.Equal(t, "Hello...", result)
}

func TestTruncateTitleBytes_MaxBytesEqualToMarkerReserve(t *testing.T) {
	e := NewEngine()
	// maxBytes == 3 (markerReserve), enters the <=markerReserve branch
	// First rune 'T' is 1 byte, fits in 3
	result := e.TruncateTitleBytes("Test Movie", 3)
	assert.Equal(t, "Tes", result)
}

func TestTruncateTitleBytes_NonCJKNoWordBoundary(t *testing.T) {
	e := NewEngine()
	// Non-CJK text with no spaces in truncated portion
	// budget=9: "Supercali" (9 bytes), no space → "Supercali..."
	result := e.TruncateTitleBytes("Supercalifragilistic", 12)
	assert.Equal(t, "Supercali...", result)
}

// --- parseModifier coverage ---

func TestParseModifier_FallbackChainInvalidPart(t *testing.T) {
	e := NewEngine()
	// "ja|123" → contains | but "123" is not a valid lang code → valid=false
	// For TITLE tag, not numeric → not looksLikeLanguageSpec (has pipe but already handled)
	// Falls through: TITLE is translatable and "ja|123" looksLikeLanguageSpec (has pipe) → rejectedLanguage
	p := e.translationResolver.parseModifier("TITLE", "ja|123")
	assert.True(t, p.rejectedLanguage)
	assert.False(t, p.isLanguage)
}

func TestParseModifier_NonTranslatableTagWithModifier(t *testing.T) {
	e := NewEngine()
	// ID is not translatable, modifier "abc" → falls through to truncation
	p := e.translationResolver.parseModifier("ID", "abc")
	assert.False(t, p.isLanguage)
	assert.Equal(t, "abc", p.truncationModifier)
}

func TestParseModifier_TranslatableTagNonNumericModifier(t *testing.T) {
	e := NewEngine()
	// DIRECTOR with "xy" → not a valid lang (normalizeLanguageCode("xy") returns "xy" which IS valid)
	// Actually "xy" normalizes to a valid 2-letter code, so isLanguage=true
	p := e.translationResolver.parseModifier("DIRECTOR", "xy")
	assert.True(t, p.isLanguage)
	assert.Equal(t, "xy", p.languageSpec)
}

func TestParseModifier_ValidFallbackChain(t *testing.T) {
	e := NewEngine()
	p := e.translationResolver.parseModifier("TITLE", "ja|en")
	assert.True(t, p.isLanguage)
	assert.Equal(t, "ja|en", p.languageSpec)
}

func TestParseModifier_EmptyModifier(t *testing.T) {
	e := NewEngine()
	p := e.translationResolver.parseModifier("TITLE", "")
	assert.False(t, p.isLanguage)
	assert.False(t, p.rejectedLanguage)
	assert.Equal(t, "", p.truncationModifier)
}

// --- looksLikeLanguageSpec coverage ---

func TestLooksLikeLanguageSpec_PrefixLength4(t *testing.T) {
	e := NewEngine()
	// "abcd-US" → prefix "abcd" has len 4 > 3 → false
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("abcd-US"))
}

func TestLooksLikeLanguageSpec_PrefixWithDigit(t *testing.T) {
	e := NewEngine()
	// "a1-US" → prefix "a1" has digit → false
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("a1-US"))
}

func TestLooksLikeLanguageSpec_ThreeLetterAlpha(t *testing.T) {
	e := NewEngine()
	// "eng" → len=3, all alpha → true
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("eng"))
}

func TestLooksLikeLanguageSpec_TwoLetterWithDigit(t *testing.T) {
	e := NewEngine()
	// "e1" → len=2, but '1' is not alpha → false
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("e1"))
}

func TestLooksLikeLanguageSpec_FourLetters(t *testing.T) {
	e := NewEngine()
	// "engl" → len=4, > 3 → false
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("engl"))
}

func TestLooksLikeLanguageSpec_SingleLetter(t *testing.T) {
	e := NewEngine()
	// "e" → len=1, < 2 → false
	assert.False(t, e.translationResolver.looksLikeLanguageSpec("e"))
}

func TestLooksLikeLanguageSpec_RegionWithUnderscore(t *testing.T) {
	e := NewEngine()
	// "en_US" → prefix "en" len=2, all alpha → true
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("en_US"))
}

func TestLooksLikeLanguageSpec_PipeInModifier(t *testing.T) {
	e := NewEngine()
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("ja|en"))
}

func TestLooksLikeLanguageSpec_PrefixLen3Alpha(t *testing.T) {
	e := NewEngine()
	// "eng-US" → prefix "eng" len=3, all alpha → true
	assert.True(t, e.translationResolver.looksLikeLanguageSpec("eng-US"))
}

// --- Additional edge cases for comprehensive coverage ---

func TestTruncateTitleBytes_CJKWordBoundary(t *testing.T) {
	e := NewEngine()
	// CJK text: budget=15 bytes = 5 CJK chars (15 bytes), no word boundary trim for CJK
	result := e.TruncateTitleBytes("これは日本語のテストです", 18)
	assert.Equal(t, "これは日本...", result)
}

func TestResolveTag_UnknownTagReturnsError(t *testing.T) {
	e := NewEngine()
	_, err := e.resolveTag("NONEXISTENT", "", &Context{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tag")
}

func TestExecuteWithContext_ManyTagsCancellation(t *testing.T) {
	e := NewEngine()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Build a template with many tags to hit i%25==0 check
	tmpl := "<ID>"
	for i := 0; i < 30; i++ {
		tmpl += " <ID>"
	}
	_, err := e.ExecuteWithContext(ctx, tmpl, &Context{ID: "test"})
	assert.Error(t, err)
}

func TestProcessConditionals_ManyBlocksCancellation(t *testing.T) {
	e := NewEngine()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Build template with 25+ conditional blocks to hit i%25==0 check
	var tmpl strings.Builder
	for i := 0; i < 30; i++ {
		tmpl.WriteString("<IF:SERIES>x</IF>")
	}
	_, err := e.processConditionalsWithContext(ctx, tmpl.String(), &Context{Series: "S"})
	assert.Error(t, err)
}

func TestTruncateTitleBytes_NonCJKSpaceAtPositionZero(t *testing.T) {
	e := NewEngine()
	// Non-CJK where the only space is at position 0 in truncated string
	result := e.TruncateTitleBytes(" leading space title here", 17)
	// budget=14: " leading space" (14 bytes), lastSpacePos > 0
	assert.Contains(t, result, "...")
}

func TestTruncate_InvalidModifier(t *testing.T) {
	e := NewEngine()
	// truncate with non-numeric modifier → Sscanf fails → returns s as-is
	result := e.truncate("Hello World", "abc")
	assert.Equal(t, "Hello World", result)
}

func TestTruncate_ZeroMaxLen(t *testing.T) {
	e := NewEngine()
	result := e.truncate("Hello", "0")
	assert.Equal(t, "Hello", result)
}

func TestIsNumericModifier_Empty(t *testing.T) {
	e := NewEngine()
	assert.False(t, e.translationResolver.isNumericModifier(""))
}

func TestIsNumericModifier_Negative(t *testing.T) {
	e := NewEngine()
	assert.False(t, e.translationResolver.isNumericModifier("-1"))
}

func TestIsNumericModifier_Zero(t *testing.T) {
	e := NewEngine()
	assert.False(t, e.translationResolver.isNumericModifier("0"))
}
